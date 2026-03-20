// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"github.com/algoritma-dev/orobox/internal/docker"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

var consoleCmd = &cobra.Command{
	Use:                "console [args]",
	Short:              "Run bin/console in the application container",
	DisableFlagParsing: true,
	Run: func(_ *cobra.Command, args []string) {
		docker.EnsureDockerCompose()
		runConsole(args)
	},
}

func init() {
	rootCmd.AddCommand(consoleCmd)
}

var runConsole = func(args []string) {
	composeCmd := docker.GetComposeCommand()
	binary, err := exec.LookPath(composeCmd[0])
	if err != nil {
		panic(err)
	}

	baseArgs := docker.GetBaseComposeArgs()
	fullArgs := append(composeCmd, baseArgs...)
	fullArgs = append(fullArgs, "exec", "application", "bin/console")
	fullArgs = append(fullArgs, args...)
	env := os.Environ()

	err = syscall.Exec(binary, fullArgs, env)
	if err != nil {
		panic(err)
	}
}
