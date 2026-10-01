// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell [service]",
	Short: "Interactive access to the container",
	Args:  cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		docker.EnsureDockerCompose()
		service := "application"
		if len(args) > 0 {
			service = args[0]
		}
		runInteractiveShell(service)
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

var runInteractiveShell = func(service string) {
	composeCmd := docker.GetComposeCommand()
	binary, err := exec.LookPath(composeCmd[0])
	if err != nil {
		panic(err)
	}

	// An unusable compose override must stop the command: exec'ing compose without it would
	// open a shell in a stack that is not the one the user described.
	if err := docker.OverrideError(); err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	baseArgs := docker.GetBaseComposeArgs()
	args := append(composeCmd, baseArgs...)
	args = append(args, "exec", service, "bash")
	env := os.Environ()

	err = syscall.Exec(binary, args, env)
	if err != nil {
		panic(err)
	}
}
