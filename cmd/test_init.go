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
	Run: func(_ *cobra.Command, _ []string) {
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
			return
		}

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

		if err := docker.EnsureServicesRunning(serviceNames); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
			return
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
				return
			}
		}

		// Drop and create database to ensure clean state
		docker.SetDatabaseInitializedCache(true, false)

		// Try psql first with FORCE (requires Postgres 13+)
		dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName)
		dropArgs := []string{"exec", "-T", "db-test", "psql", "-U", dbUser, "-d", "postgres", "-c", dropSQL}
		if err := docker.RunComposeCommandSilently("Dropping test database...", dropArgs...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to drop test database: %v", err))
			return
		}

		createCmd := []string{"run", "--rm", "-T", "application", "php", "bin/console", "doctrine:database:create", "--env=test", "--if-not-exists"}
		if err := docker.RunComposeCommandSilently("Creating test database...", createCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("failed to create test database: %v", err))
			return
		}

		uuidExtensionSQL := "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";"
		uuidExtensionArgs := []string{"exec", "-T", "db-test", "psql", "-U", dbUser, "-d", dbName, "-c", uuidExtensionSQL}
		if err := docker.RunComposeCommandSilently("Creating uuid extension...", uuidExtensionArgs...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to create uuid extension: %v", err))
			return
		}

		clearCacheCmd := []string{"run", "--rm", "-T", "application", "bash", "-c", "rm -rf var/cache/test"}
		if err := docker.RunComposeCommandSilently("Clearing cache for test environment...", clearCacheCmd...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to clear cache: %v", err))
		}

		installCmd := []string{"run", "--rm", "-T", "application", "php", "bin/console", "oro:install", "--no-interaction", "--env=test", "--skip-translations"}
		if err := docker.RunComposeCommandSilently("Running Oro installation for test environment (this may take several minutes)...", installCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("test environment installation failed: %v", err))
			return
		}

		docker.SetDatabaseInitializedCache(true, true)
		utils.PrintSuccess("Test environment initialized successfully!")

		utils.PrintTitle("Test Database Connection (e.g. PhpStorm):")
		fmt.Println("  - Host: localhost")
		fmt.Println("  - Port: 5433")
		fmt.Printf("  - User: %s\n", dbUser)
		fmt.Printf("  - Password: %s\n", dbPass)
		fmt.Printf("  - Database: %s\n", dbName)
	},
}

func init() {
	testInitCmd.Flags().BoolVar(&testInitUseTmpfs, "tmpfs", false, "Initialize in RAM the database instead of disk")
	testInitCmd.Flags().StringVar(&testInitTmpfsSize, "tmpfs-size", "1g", "Size of the tmpfs mount")
	rootCmd.AddCommand(testInitCmd)
}
