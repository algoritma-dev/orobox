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
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()
		if err := docker.RunComposeCommandSilently("down"); err != nil {
			utils.PrintError(fmt.Sprintf("Shut down failed: %v", err))
			return
		}
		utils.PrintSuccess("Environment shut down.")
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
