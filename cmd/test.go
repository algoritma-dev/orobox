// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"orobox/internal/config"
	"orobox/internal/docker"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests (PHPUnit)",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("Running tests...")
		runTestCommand()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTestCommand() {
	var args []string
	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	args = append(args, "application")

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
