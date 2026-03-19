// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"github.com/algoritma-dev/orobox/internal/docker"
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

	baseArgs := docker.GetBaseComposeArgs()
	args := append(composeCmd, baseArgs...)
	args = append(args, "exec", service, "bash")
	env := os.Environ()

	err = syscall.Exec(binary, args, env)
	if err != nil {
		panic(err)
	}
}
