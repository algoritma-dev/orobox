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

// shellExecArgs returns the compose arguments that open an interactive shell in service. bash is
// preferred, but not every image ships it — a service a project adds may be a busybox or alpine
// image with only sh — so the command falls back to sh rather than failing with "bash: not found".
func shellExecArgs(service string) []string {
	args := docker.GetBaseComposeArgs()
	return append(args, "exec", service, "sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh")
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

	args := append(composeCmd, shellExecArgs(service)...)
	env := os.Environ()

	err = syscall.Exec(binary, args, env)
	if err != nil {
		panic(err)
	}
}
