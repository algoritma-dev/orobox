// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Shut down the environment",
	// See upCmd: a compose failure is not a usage error.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		docker.SetIncludeTestFiles(true)
		docker.EnsureDockerCompose()
		if err := docker.RunComposeCommandSilently("Stopping containers...", "down", "--remove-orphans"); err != nil {
			utils.PrintError(fmt.Sprintf("Shut down failed: %v", err))
			return err
		}
		utils.PrintSuccess("Environment shut down.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
