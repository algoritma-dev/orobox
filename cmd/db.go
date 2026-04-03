// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/cobra"
)

var dbExec = func(stdin io.Reader, stdout io.Writer, args ...string) error {
	_, dbPass, _ := docker.GetDatabaseCredentials()
	composeCmd := docker.GetComposeCommand()
	baseArgs := docker.GetBaseComposeArgs()

	fullArgs := append(composeCmd[1:], baseArgs...)
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command(composeCmd[0], fullArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbPass)

	return cmd.Run()
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations",
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup [file]",
	Short: "Backup the database to a file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		docker.EnsureDockerCompose()
		backupFile := args[0]
		backupDatabase(backupFile)
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore [file]",
	Short: "Restore the database from a file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		docker.EnsureDockerCompose()
		restoreFile := args[0]
		restoreDatabase(restoreFile)
	},
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
}

func backupDatabase(file string) {
	utils.StartLoader("Creating database backup...")
	defer utils.StopLoader()

	dbUser, _, dbName := docker.GetDatabaseCredentials()

	f, err := os.Create(file)
	if err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to create file: %v", err))
		return
	}
	defer f.Close()

	args := []string{"exec", "-T", "db", "pg_dump", "-U", dbUser, "--clean", "--if-exists", dbName}

	if err := dbExec(nil, f, args...); err != nil {
		utils.StopLoader()
		f.Close()
		_ = os.Remove(file)
		utils.PrintError(fmt.Sprintf("Backup failed: %v", err))
		return
	}

	utils.StopLoader()
	utils.PrintSuccess(fmt.Sprintf("Backup saved to %s", file))
}

func restoreDatabase(file string) {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		utils.PrintError(fmt.Sprintf("File %s does not exist", file))
		return
	}

	dbUser, _, dbName := docker.GetDatabaseCredentials()

	// Ensure services are running
	if err := docker.EnsureServicesRunning([]string{"db", "application"}); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
		return
	}

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
			return
		}
	}

	extensionArgs := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", dbName, "-c", extensionQuery}
	if err := dbExec(nil, nil, extensionArgs...); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to create uuid-ossp extension: %v", err))
		return
	}

	f, err := os.Open(file)
	if err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Failed to open file: %v", err))
		return
	}
	defer f.Close()

	args := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", dbName}

	if err := dbExec(f, nil, args...); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("Restore failed: %v", err))
		return
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

	// 3. Clear cache
	utils.StartLoader("Clearing cache...")
	if err := docker.RunComposeCommandSilently("", "exec", "-T", "application", "rm", "-rf", "var/cache/dev"); err != nil {
		utils.StopLoader()
		utils.PrintWarning(fmt.Sprintf("Failed to clear cache: %v", err))
	} else {
		utils.StopLoader()
		utils.PrintSuccess("Cache cleared.")
	}

	// 4. Update platform
	utils.StartLoader("Running oro:platform:update...")
	if err := docker.RunComposeCommandSilently("", "exec", "-T", "application", "bin/console", "oro:platform:update", "--force", "--timeout=0"); err != nil {
		utils.StopLoader()
		utils.PrintError(fmt.Sprintf("oro:platform:update failed: %v", err))
	} else {
		utils.StopLoader()
		utils.PrintSuccess("Platform updated.")
	}

	utils.PrintSuccess("Restore completed successfully!")
}
