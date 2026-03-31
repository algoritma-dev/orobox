// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"strings"

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
	// Ensure application_test container is running
	upArgs := []string{"up", "-d", "db_test", "application_test"}
	if err := docker.RunComposeCommandSilently("Starting test application...", upArgs...); err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to ensure application_test is running: %v", err))
	}

	// Check if database schema exists
	checkArgs := []string{
		"exec", "-T", "db_test",
		"psql",
		"-U", "oro_db_user", // postgres user
		"-lqt", // list databases in quiet format
	}
	utils.StartLoader("Checking test environment...")
	databases, err := docker.RunComposeCommandWithOutput(checkArgs...)

	// Find db_name in output (can be overridden by ORO_DB_NAME_TEST)
	_, _, dbName := docker.GetDatabaseTestCredentials()
	found := false
	for _, line := range strings.Split(string(databases), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) > 0 && strings.TrimSpace(fields[0]) == dbName {
			found = true
			break
		}
	}

	utils.StopLoader()
	if err != nil || !found {
		utils.PrintError("Test database schema is not initialized or incomplete.")
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
