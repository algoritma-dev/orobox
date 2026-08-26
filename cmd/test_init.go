package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	yamlv3 "gopkg.in/yaml.v3"
)

var testInitUseTmpfs bool
var testInitTmpfsSize string

var testInitCmd = &cobra.Command{
	Use:   "test-init",
	Short: "Initialize or reset the test environment",
	// A database that cannot be provisioned is a runtime problem, not a usage problem.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		docker.SetIncludeTestFiles(true)
		if testInitUseTmpfs {
			viper.Set("test.use_tmpfs", true)
			viper.Set("test.tmpfs_size", testInitTmpfsSize)
			var conf config.OroConfig
			if err := viper.Unmarshal(&conf); err == nil {
				conf.Test.UseTmpfs = true
				conf.Test.TmpfsSize = testInitTmpfsSize
				data, err := yamlv3.Marshal(&conf)
				if err == nil {
					_ = os.WriteFile(".orobox.yaml", data, 0644)
				}
			}
		}

		docker.EnsureDockerCompose()

		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			return err
		}

		// Everything the test installation needs is started explicitly here, because the
		// one-off containers below run with --no-deps (see testInitRunArgs).
		serviceNames := []string{"db-test"}
		if conf.Services.Redis {
			serviceNames = append(serviceNames, "redis")
		}
		if conf.Services.RabbitMQ {
			serviceNames = append(serviceNames, "rabbitmq")
		}
		if conf.Services.Elasticsearch {
			serviceNames = append(serviceNames, "elasticsearch")
		}
		if conf.Services.Mailpit {
			// Previously started as a transitive dependency of `application`; keep it so
			// the test install still has a mailer to point at.
			serviceNames = append(serviceNames, "mail")
		}

		if err := docker.EnsureServicesRunning(serviceNames); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
			return err
		}

		// EnsureServicesRunning returns once the container is running, which is earlier than
		// Postgres listening: every psql below would otherwise race the socket appearing.
		if err := docker.WaitForDatabaseReady(true); err != nil {
			utils.PrintError(err.Error())
			return err
		}

		// Check if already initialized
		dbUser, dbPass, dbName, _ := docker.GetDatabaseTestCredentials()
		utils.StartLoader("Checking for existing installation...")
		isInstalled, err := docker.IsDatabaseInitialized(true)
		utils.StopLoader()

		if err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to check database status: %v", err))
		}

		if isInstalled {
			reader := bufio.NewReader(os.Stdin)
			if !utils.AskYesNo(reader, "Test environment is already initialized. Do you want to reset it?", false) {
				utils.PrintInfo("Aborted.")
				return nil
			}
		}

		// Drop and create database to ensure clean state
		docker.SetDatabaseInitializedCache(true, false)

		// The next two steps used to warn and return, which abandoned the whole
		// initialization behind a zero exit code: the test database was left un-provisioned
		// and the failure only resurfaced later, as `orobox test` reporting an uninitialized
		// test environment. They are errors — the command did not do what it was asked.
		//
		// Try psql first with FORCE (requires Postgres 13+)
		dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName)
		dropArgs := []string{"exec", "-T", "db-test", "psql", "-U", dbUser, "-d", "postgres", "-c", dropSQL}
		if err := docker.RunComposeCommandSilently("Dropping test database...", dropArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("failed to drop test database: %v", err))
			return err
		}

		createCmd := testInitRunArgs("php", "bin/console", "doctrine:database:create", "--env=test", "--if-not-exists")
		if err := docker.RunComposeCommandSilently("Creating test database...", createCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("failed to create test database: %v", err))
			return err
		}

		uuidExtensionSQL := "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";"
		uuidExtensionArgs := []string{"exec", "-T", "db-test", "psql", "-U", dbUser, "-d", dbName, "-c", uuidExtensionSQL}
		if err := docker.RunComposeCommandSilently("Creating uuid extension...", uuidExtensionArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("failed to create uuid extension: %v", err))
			return err
		}

		clearCacheCmd := testInitRunArgs("bash", "-c", "rm -rf var/cache/test")
		if err := docker.RunComposeCommandSilently("Clearing cache for test environment...", clearCacheCmd...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to clear cache: %v", err))
		}

		installCmd := testInitRunArgs("php", "bin/console", "oro:install", "--no-interaction", "--env=test", "--skip-translations")
		if err := docker.RunComposeCommandSilently("Running Oro installation for test environment (this may take several minutes)...", installCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("test environment installation failed: %v", err))
			return err
		}

		docker.SetDatabaseInitializedCache(true, true)
		if err := docker.EnsureServicesRunning([]string{"application"}); err != nil {
			utils.PrintError(fmt.Sprintf("failed to start test application container: %v", err))
			return err
		}
		utils.PrintSuccess("Test environment initialized successfully!")

		utils.PrintTitle("Test Database Connection (e.g. PhpStorm):")
		fmt.Println("  - Host: localhost")
		fmt.Println("  - Port: 5433")
		fmt.Printf("  - User: %s\n", dbUser)
		fmt.Printf("  - Password: %s\n", dbPass)
		fmt.Printf("  - Database: %s\n", dbName)
		return nil
	},
}

// testInitRunArgs builds a one-off `docker compose run` in the application service for the
// test environment setup, appending the given command.
//
// --no-deps keeps this from starting the dev stack. The `application` service depends_on
// web, consumer and cron, so without it Compose would boot those (plus php-fpm-app, ws and
// the dev `db`) even though the test installation only talks to db-test. Beyond being
// wasteful, consumer and cron restart-loop whenever they cannot reach an installed database
// and each retry rewrites var/cache — the race that used to break `orobox init`
// (see initRunArgs in init.go). The services the test install needs are started explicitly
// via EnsureServicesRunning above.
func testInitRunArgs(command ...string) []string {
	args := []string{"run", "--rm", "-T", "--no-deps", "application"}
	return append(args, command...)
}

func init() {
	testInitCmd.Flags().BoolVar(&testInitUseTmpfs, "tmpfs", false, "Initialize in RAM the database instead of disk")
	testInitCmd.Flags().StringVar(&testInitTmpfsSize, "tmpfs-size", "1g", "Size of the tmpfs mount")
	rootCmd.AddCommand(testInitCmd)
}
