// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/cobra"
)

var dbExec = func(stdin io.Reader, stdout io.Writer, args ...string) error {
	_, dbPass, _, _ := docker.GetDatabaseCredentials()
	composeCmd := docker.GetComposeCommand()
	baseArgs := docker.GetBaseComposeArgs()

	fullArgs := append(composeCmd[1:], baseArgs...)
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command(composeCmd[0], fullArgs...)
	docker.PrintDebugCommand(composeCmd[0], fullArgs)
	cmd.Stdin = stdin

	var stdoutBuf, stderrBuf bytes.Buffer
	if stdout == nil {
		cmd.Stdout = &stdoutBuf
	} else {
		cmd.Stdout = stdout
	}
	cmd.Stderr = &stderrBuf

	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbPass)

	err := cmd.Run()
	if err != nil {
		utils.StopLoader()
		if stderrBuf.Len() > 0 {
			fmt.Fprint(os.Stderr, stderrBuf.String())
		}
		if stdout == nil && stdoutBuf.Len() > 0 {
			fmt.Fprint(os.Stdout, stdoutBuf.String())
		}
		return err
	}
	return nil
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations",
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup [file]",
	Short: "Backup the database to a file",
	Args:  cobra.ExactArgs(1),
	// A pg_dump that fails is a runtime problem, not a usage problem.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, args []string) error {
		docker.EnsureDockerCompose()
		return backupDatabase(args[0])
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore [file]",
	Short: "Restore the database from a file",
	Args:  cobra.ExactArgs(1),
	// See dbBackupCmd.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, args []string) error {
		docker.EnsureDockerCompose()
		return restoreDatabase(args[0])
	},
}

// cacheClearScript empties var/cache/dev without racing the processes that write into it: it
// detaches the directory with a rename and only then deletes it. Left as a shell snippet
// because both steps have to happen in the container, in that order, in one exec.
//
// The fresh directory is recreated by the same exec rather than left to whoever gets there first.
// var/cache is a volume every Oro container shares, so the directory's mode is whatever the
// winning process's umask made it; creating it here means it is always the application container's.
const cacheClearScript = `set -e
dir=var/cache/dev
if [ -d "$dir" ]; then
  old="$dir.orobox-old.$$"
  mv "$dir" "$old"
  mkdir -p "$dir"
  rm -rf "$old"
fi`

// oroKernelServices are the long-running services that boot an Oro kernel of their own against
// the same var/cache volume the application container writes to.
//
// They are stopped for the duration of a restore, and that is not only about the cache. A restore
// drops and recreates the database under them: a consumer mid-message, a cron job mid-run or a
// request being served against the schema that is being replaced has nothing valid to do, and web
// is in the list for the same reason as the rest — its health probe alone is enough to boot a
// kernel every few seconds.
//
// The cache is what turns that from untidy into a failure. Once cacheClearScript has detached
// var/cache/dev, every one of these processes starts rebuilding it concurrently with
// oro:platform:update, and Oro's extend-class writer creates its directories without tolerating
// the EEXIST a second creator hands it: "Could not create cache directory
// .../var/cache/dev/oro_entities/Extend/Entity", and the restore fails on a race rather than on
// anything about the dump.
var oroKernelServices = []string{"web", "php-fpm-app", "ws", "consumer", "cron"}

// quiesceOroKernels stops the services in oroKernelServices that are currently running and returns
// the function that starts exactly those back up.
//
// Only the running ones are touched, so a restore never leaves a stack with more services up than
// it found — a developer who deliberately keeps the consumer down gets it back down afterwards.
// Both halves are best-effort: failing a restore because a background service could not be paused
// would trade a rare race for a certain outage.
func quiesceOroKernels() func() {
	running := docker.RunningServices(oroKernelServices)
	if len(running) == 0 {
		return func() {}
	}

	if err := docker.RunComposeCommandSilently("", append([]string{"stop"}, running...)...); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not pause %s during the restore: %v", strings.Join(running, ", "), err))
		return func() {}
	}

	return func() {
		if err := docker.RunComposeCommandSilently("", append([]string{"start"}, running...)...); err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not start %s back up: %v. Run 'orobox up'.", strings.Join(running, ", "), err))
		}
	}
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
}

// backupDatabase returns the error rather than only printing it: a backup that produced no
// usable dump must not report success to whoever runs it, and a caller writing a dump into a
// deploy or CI step reads the exit code.
func backupDatabase(file string) error {
	utils.StartLoader("Creating database backup...")
	defer utils.StopLoader()

	dbUser, _, dbName, _ := docker.GetDatabaseCredentials()

	f, err := os.Create(file)
	if err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to create file: %v", err))
		return err
	}
	defer f.Close()

	args := []string{"exec", "-T", "db", "pg_dump", "-U", dbUser, "--clean", "--if-exists", dbName}

	if err := dbExec(nil, f, args...); err != nil {
		utils.StopLoader()
		f.Close()
		_ = os.Remove(file)
		utils.PrintError(fmt.Sprintf("Backup failed: %v", err))
		return err
	}

	utils.StopLoader()
	utils.PrintSuccess(fmt.Sprintf("Backup saved to %s", file))
	return nil
}

// restoreDatabase returns the error rather than only printing it: every step below leaves the
// database in a state the caller has to know about, and a restore that stopped halfway must not
// look like a successful one.
func restoreDatabase(file string) error {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		utils.PrintError(fmt.Sprintf("File %s does not exist", file))
		return fmt.Errorf("backup file %s does not exist", file)
	}

	dbUser, _, dbName, _ := docker.GetDatabaseCredentials()

	// Ensure services are running
	if err := docker.EnsureServicesRunning([]string{"db", "application"}); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
		return err
	}

	// The drop/create/restore below are all psql, so wait for the server and not just for
	// the container.
	if err := docker.WaitForDatabaseReady(false); err != nil {
		utils.PrintError(err.Error())
		return err
	}

	// Nothing else may hold a kernel open while the database and the cache underneath it are
	// replaced; see oroKernelServices. Deferred, so a restore that fails halfway still hands the
	// stack back in the shape it found it.
	resume := quiesceOroKernels()
	defer resume()

	// 1. Restore the database
	utils.StartLoader("Restoring database...")

	// Clear the database before restoration to avoid "already exists" errors
	// We use DROP DATABASE instead of DROP SCHEMA CASCADE to avoid "max_locks_per_transaction" issues with many tables
	terminateQuery := fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();", dbName)
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbName)
	createQuery := fmt.Sprintf("CREATE DATABASE %s;", dbName)
	extensionQuery := "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";"

	for _, q := range []string{terminateQuery, dropQuery, createQuery} {
		clearArgs := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", "postgres", "-c", q}
		if err := dbExec(nil, nil, clearArgs...); err != nil && q != terminateQuery {
			utils.StopLoader()
			utils.PrintError(fmt.Sprintf("Failed to clear database: %v", err))
			return err
		}
	}

	extensionArgs := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", dbName, "-c", extensionQuery}
	if err := dbExec(nil, nil, extensionArgs...); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to create uuid-ossp extension: %v", err))
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to open file: %v", err))
		return err
	}
	defer f.Close()

	args := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", dbName}

	if err := dbExec(f, nil, args...); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Restore failed: %v", err))
		return err
	}
	utils.StopLoader()
	utils.PrintSuccess("Database restored.")

	// 2. Update configuration URLs
	utils.StartLoader("Updating configuration URLs...")
	urls := docker.GetApplicationURLs()
	if len(urls) == 0 {
		utils.StopLoader()
		utils.PrintWarning("No application URLs configured, skipping configuration update.")
	} else {
		primaryDomain := urls[0]

		// Requirement: update records with name = application_url, url, secure_url in oro_config_values
		// We use both oro_config_values and oro_config_value for safety.

		updateQueries := []string{
			fmt.Sprintf("UPDATE oro_config_value SET text_value = '%s' WHERE name IN ('application_url', 'url', 'secure_url');", primaryDomain),
		}

		for _, q := range updateQueries {
			queryArgs := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", dbName, "-c", q}
			_ = dbExec(nil, nil, queryArgs...)
		}
		utils.StopLoader()
		utils.PrintSuccess("Configuration URLs updated.")
	}

	// 3. Clear cache.
	//
	// The rename is the part that matters: a restore runs against a live stack, so web,
	// consumer and cron keep warming the cache while the directory is being removed. A plain
	// `rm -rf var/cache/dev` then walks a tree that grows underneath it and fails with
	// "Directory not empty". After the rename those writers resolve var/cache/dev afresh and
	// repopulate a new directory, and the detached tree can be removed with nothing writing
	// into it.
	utils.StartLoader("Clearing cache...")
	if err := docker.RunComposeCommandSilently("", "exec", "-T", "application", "sh", "-c", cacheClearScript); err != nil {
		utils.StopLoader()
		utils.PrintWarning(fmt.Sprintf("Failed to clear cache: %v", err))
	} else {
		utils.StopLoader()
		utils.PrintSuccess("Cache cleared.")
	}

	// 4. Update platform
	// The restore is not finished until the schema matches the code: reporting
	// "Restore completed successfully!" after this failed left a database the application
	// cannot boot against behind a zero exit code.
	utils.StartLoader("Running oro:platform:update...")
	if err := docker.RunComposeCommandSilently("", "exec", "-T", "application", "bin/console", "oro:platform:update", "--force", "--timeout=0"); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("oro:platform:update failed: %v", err))
		return err
	}
	utils.StopLoader()
	utils.PrintSuccess("Platform updated.")

	utils.PrintSuccess("Restore completed successfully!")
	return nil
}
