// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"github.com/algoritma-dev/orobox/internal/docker"

	"github.com/spf13/cobra"
)

var cleanBeforeUp bool

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the development environment",
	Run: func(_ *cobra.Command, _ []string) {
		if cleanBeforeUp {
			fmt.Println("Cleaning up environment before starting...")
			if err := docker.RunComposeCommand("down", "-v", "--remove-orphans"); err != nil {
				fmt.Printf("Warning: failed to clean up: %v\n", err)
			}
		}

		fmt.Println("Environment started!")
		fmt.Println("Running OroCommerce bootstrap...")

		upArgs := []string{"up"}

		if err := docker.RunComposeCommand(append(upArgs, "install")...); err != nil {
			fmt.Printf("Bootstrap failed: %v\n", err)
			return
		}
		if err := docker.RunComposeCommand(append(upArgs, "application")...); err != nil {
			fmt.Printf("Bootstrap failed: %v\n", err)
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&cleanBeforeUp, "clean", "c", false, "Clean up environment before starting")
}
