// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"github.com/algoritma-dev/orobox/internal/docker"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Shut down the environment",
	Run: func(_ *cobra.Command, _ []string) {
		if err := docker.RunComposeCommand("down"); err != nil {
			fmt.Printf("Shut down failed: %v\n", err)
			return
		}
		fmt.Println("Environment shut down.")
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
