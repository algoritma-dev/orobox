// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove all containers and volumes to start fresh",
	// See upCmd: a compose failure is not a usage error.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		docker.EnsureDockerCompose()
		if err := docker.RunComposeCommandSilently("Cleaning up containers and volumes...", "down", "-v", "--remove-orphans"); err != nil {
			utils.PrintError(fmt.Sprintf("Cleanup failed: %v", err))
			return err
		}
		utils.PrintSuccess("Cleanup complete.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
