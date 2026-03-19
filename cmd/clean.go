// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"orobox/internal/docker"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all containers and volumes to start fresh",
	Run: func(_ *cobra.Command, args []string) {
		fmt.Println("Cleaning up environment (removing containers and volumes)...")
		if err := docker.RunComposeCommand("down", "-v", "--remove-orphans"); err != nil {
			fmt.Printf("Cleanup failed: %v\n", err)
			return
		}
		fmt.Println("Cleanup complete.")
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
