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

var filter string
var testsuite string

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
	testCmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter tests by name")
	testCmd.Flags().StringVarP(&testsuite, "testsuite", "t", "", "Run specific test suite")
	rootCmd.AddCommand(testCmd)
}

func runTestCommand() {
	// Ensure services are running
	if err := docker.EnsureServicesRunning([]string{"db_test", "application_test"}); err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to ensure services are running: %v", err))
	}

	// Check if database is initialized
	utils.StartLoader("Checking test environment...")
	isInstalled, err := docker.IsDatabaseInitialized(true)
	utils.StopLoader()

	if err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to check database status: %v", err))
		// We proceed anyway, PHPUnit will fail later with a better error message if it's really down
	}

	if !isInstalled {
		utils.PrintError("Test database is not initialized.")
		utils.PrintInfo("Please run 'orobox test-init' to prepare the test environment.")
		return
	}

	var args []string
	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	args = append(args, "application_test")

	if viper.GetString("type") == "bundle" {
		bundlePath := "src/" + config.GetBundlePath()
		args = append(args, "./bin/simple-phpunit", "--configuration="+bundlePath)
	} else {
		args = append(args, "php", "bin/phpunit")
	}

	if filter != "" {
		args = append(args, "--filter", filter)
	}
	if testsuite != "" {
		args = append(args, "--testsuite", testsuite)
	}

	err = docker.RunComposeCommand("", args...)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Tests reported errors: %v", err))
		os.Exit(1)
	} else {
		utils.PrintSuccess("Tests completed successfully!")
	}
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
