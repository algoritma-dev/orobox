// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests (PHPUnit)",
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()
		utils.PrintInfo("Running tests...")
		runTestCommand()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTestCommand() {
	// Ensure application_test container is running
	if err := docker.RunComposeCommandSilently("up", "-d", "application_test"); err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to ensure application_test is running: %v", err))
	}

	// Check if database schema exists
	fmt.Print("Checking test environment... ")
	checkArgs := []string{"exec", "-T", "application_test", "php", "bin/console", "doctrine:query:sql", "SELECT 1 FROM oro_user LIMIT 1", "--env=test"}
	if _, err := docker.RunComposeCommandWithOutput(checkArgs...); err != nil {
		fmt.Println("NOT FOUND")
		utils.PrintError("Test database schema is not initialized or incomplete.")
		utils.PrintInfo("Please run 'orobox test-init' to prepare the test environment.")
		return
	}
	fmt.Println("OK")

	var args []string
	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	// Set test environment
	args = append(args, "-e", "APP_ENV=test")
	args = append(args, "-e", "ORO_ENV=test")
	args = append(args, "-e", "ORO_DB_NAME=oro_db_test")

	args = append(args, "application_test")

	if viper.GetString("type") == "bundle" {
		bundlePath := "src/" + config.GetBundlePath()
		args = append(args, "./bin/simple-phpunit", "--configuration="+bundlePath)
	} else {
		args = append(args, "php", "bin/phpunit")
	}

	err := docker.RunComposeCommand(args...)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Tests reported errors: %v", err))
	} else {
		utils.PrintSuccess("Tests completed successfully!")
	}
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
