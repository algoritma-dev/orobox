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
)

var testInitCmd = &cobra.Command{
	Use:   "test-init",
	Short: "Initialize or reset the test environment",
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()

		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			return
		}

		utils.PrintInfo("Starting services for test environment...")
		services := []string{"up", "-d", "db", "application_test"}
		if conf.Services.Redis {
			services = append(services, "redis")
		}
		if conf.Services.RabbitMQ {
			services = append(services, "rabbitmq")
		}
		if conf.Services.Elasticsearch {
			services = append(services, "elasticsearch")
		}

		if err := docker.RunComposeCommandSilently(services...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
			return
		}

		// Check if already initialized
		checkArgs := []string{"exec", "-T", "application_test", "php", "bin/console", "doctrine:query:sql", "SELECT 1 FROM oro_user LIMIT 1", "--env=test"}
		if _, err := docker.RunComposeCommandWithOutput(checkArgs...); err == nil {
			reader := bufio.NewReader(os.Stdin)
			if !utils.AskYesNo(reader, "Test environment is already initialized. Do you want to reset it?", false) {
				utils.PrintInfo("Aborted.")
				return
			}
		}

		utils.PrintInfo("Resetting test database...")
		// Use psql via db container to drop database more reliably (can terminate active connections)
		dbUser := viper.GetString("db_user")
		if dbUser == "" {
			dbUser = os.Getenv("ORO_DB_USER")
		}
		if dbUser == "" {
			dbUser = "oro_db_user"
		}

		// Drop and create database to ensure clean state
		// Try psql first with FORCE (requires Postgres 13+)
		dropSQL := "DROP DATABASE IF EXISTS oro_db_test WITH (FORCE);"
		dropArgs := []string{"exec", "-T", "db", "psql", "-U", dbUser, "-d", "postgres", "-c", dropSQL}
		if err := docker.RunComposeCommandSilently(dropArgs...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to drop test database using psql: %v. Trying via doctrine...", err))
			dropCmd := []string{"exec", "-T", "application_test", "php", "bin/console", "doctrine:database:drop", "--force", "--env=test", "--if-exists"}
			if err := docker.RunComposeCommandSilently(dropCmd...); err != nil {
				utils.PrintWarning(fmt.Sprintf("failed to drop test database: %v", err))
			}
		}

		createCmd := []string{"exec", "-T", "application_test", "php", "bin/console", "doctrine:database:create", "--env=test"}
		if err := docker.RunComposeCommandSilently(createCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("failed to create test database: %v", err))
			return
		}

		utils.PrintInfo("Clearing cache for test environment...")
		clearCacheCmd := []string{"exec", "-T", "application_test", "bash", "-c", "rm -rf var/cache/test"}
		if err := docker.RunComposeCommandSilently(clearCacheCmd...); err != nil {
			utils.PrintWarning(fmt.Sprintf("failed to clear cache: %v", err))
		}

		utils.PrintInfo("Running Oro installation for test environment (this may take several minutes)...")
		installCmd := []string{"exec", "-T", "application_test", "php", "bin/console", "oro:install", "--no-interaction", "--env=test", "--skip-translations"}
		if err := docker.RunComposeCommandSilently(installCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("test environment installation failed: %v", err))
			return
		}

		utils.PrintSuccess("Test environment initialized successfully!")
	},
}

func init() {
	rootCmd.AddCommand(testInitCmd)
}
