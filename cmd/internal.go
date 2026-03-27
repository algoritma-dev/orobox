package cmd

import (
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/spf13/cobra"
)

var internalGenDockerCmd = &cobra.Command{
	Use:    "internal-gen-docker",
	Short:  "Internal command to generate docker files (used in CI)",
	Hidden: true,
	Run: func(_ *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()
	},
}

func init() {
	rootCmd.AddCommand(internalGenDockerCmd)
}
