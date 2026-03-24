// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests (PHPUnit)",
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()
		fmt.Println("Running tests...")
		runTestCommand()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTestCommand() {
	// Ensure application_test container is running
	if err := docker.RunComposeCommand("up", "-d", "application_test"); err != nil {
		fmt.Printf("Warning: failed to ensure application_test is running: %v\n", err)
	}

	// Run database check and restore (or install if empty)
	if err := docker.RunComposeCommand("exec", "-T", "application_test", "/usr/local/bin/docker-entrypoint.sh", "restore"); err != nil {
		fmt.Printf("Database preparation failed: %v\n", err)
		return
	}

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
		fmt.Printf("Tests reported errors: %v\n", err)
	} else {
		fmt.Println("Tests completed successfully!")
	}
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
