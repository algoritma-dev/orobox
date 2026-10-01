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
	Use:   "logs [service...]",
	Short: "View logs from the development environment",
	Long: `View logs from different services in the development environment.

Besides the shortcut flags, any compose service can be named directly, including the
ones added in .orobox.compose.yaml: orobox logs minio`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, positional []string) {
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

		// Named services come after the flag ones. Both can mention the same service (--app
		// already includes php-fpm-app), and compose would follow it twice.
		services = dedupe(append(services, positional...))

		if len(services) == 0 {
			utils.PrintWarning("Please specify at least one service or log type: <service>, --nginx, --php, --app, --consumer, --cron, or --ws")
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

// dedupe drops repeated entries, keeping the first occurrence of each.
func dedupe(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
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
