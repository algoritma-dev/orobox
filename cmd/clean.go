// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all containers and volumes to start fresh",
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()
		if err := docker.RunComposeCommandSilently("Cleaning up containers and volumes...", "down", "-v", "--remove-orphans"); err != nil {
			utils.PrintError(fmt.Sprintf("Cleanup failed: %v", err))
			return
		}
		utils.PrintSuccess("Cleanup complete.")
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
