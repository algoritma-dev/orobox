package cmd

import (
	"fmt"
	"orobox/internal/docker"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Shut down the environment",
	Run: func(cmd *cobra.Command, args []string) {
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
