// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/cobra"
)

var (
	logsNginx    bool
	logsPhp      bool
	logsApp      bool
	logsConsumer bool
	logsCron     bool
	logsWs       bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View logs from the development environment",
	Long:  `View logs from different services in the development environment.`,
	Run: func(cmd *cobra.Command, _ []string) {
		docker.EnsureDockerCompose()

		var services []string
		if logsNginx {
			services = append(services, "web")
		}
		if logsPhp {
			services = append(services, "php-fpm-app")
		}
		if logsApp {
			// For OroCommerce/Symfony, we include the main services running PHP code
			services = append(services, "application", "php-fpm-app")
		}
		if logsConsumer {
			services = append(services, "consumer")
		}
		if logsCron {
			services = append(services, "cron")
		}
		if logsWs {
			services = append(services, "ws")
		}

		if len(services) == 0 {
			utils.PrintWarning("Please specify at least one log type: --nginx, --php, --app, --consumer, --cron, or --ws")
			_ = cmd.Help()
			return
		}

		args := append([]string{"logs", "-f"}, services...)
		if err := docker.RunComposeCommand("", args...); err != nil {
			utils.PrintError(fmt.Sprintf("Error viewing logs: %v", err))
		}

		// Reset flags for subsequent calls (important for tests)
		logsNginx = false
		logsPhp = false
		logsApp = false
		logsConsumer = false
		logsCron = false
		logsWs = false
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolVar(&logsNginx, "nginx", false, "View Nginx logs")
	logsCmd.Flags().BoolVar(&logsPhp, "php", false, "View PHP logs")
	logsCmd.Flags().BoolVar(&logsApp, "app", false, "View Symfony/OroCommerce logs")
	logsCmd.Flags().BoolVar(&logsConsumer, "consumer", false, "View Consumer logs")
	logsCmd.Flags().BoolVar(&logsCron, "cron", false, "View Cron logs")
	logsCmd.Flags().BoolVar(&logsWs, "ws", false, "View WS logs")
}
