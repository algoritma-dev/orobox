// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"

	"github.com/spf13/cobra"
)

var cleanBeforeUp bool

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the development environment",
	Run: func(_ *cobra.Command, _ []string) {
		dockerfileIsChanged := docker.EnsureDockerCompose()
		if dockerfileIsChanged {
			if err := docker.RunComposeCommandSilently("Rebuilding containers...", "build"); err != nil {
				utils.PrintError(fmt.Sprintf("Build failed: %v", err))
				return
			}
		}

		if cleanBeforeUp {
			if err := docker.RunComposeCommandSilently("Cleaning up environment...", "down", "-v", "--remove-orphans"); err != nil {
				utils.PrintWarning(fmt.Sprintf("failed to clean up: %v", err))
			}
		}

		if err := docker.RunComposeCommandSilently("Starting containers...", "up", "-d", "application"); err != nil {
			utils.PrintError(fmt.Sprintf("Startup failed: %v", err))
			return
		}

		utils.PrintSuccess("Orobox is up and running!")

		for _, url := range docker.GetApplicationURLs() {
			fmt.Printf("  - %s\n", url)
		}

		if viper.GetBool("services.mailpit") {
			utils.PrintTitle("Mailpit is available at:")
			fmt.Println("  - http://localhost:8025")
		}

		if viper.GetBool("services.php.xdebug") {
			utils.PrintTitle("Xdebug is ENABLED")
			fmt.Println("To debug in PhpStorm:")
			fmt.Println("  1. Ensure the 'Phone' icon (Listener) is ON.")
			fmt.Println("  2. Configure Path Mappings in Settings -> PHP -> Servers:")
			fmt.Printf("     - Host: %s\n", "oro.demo (or your custom domain)")
			fmt.Printf("     - Local Path: %s\n", config.GetHostBundlePath())
			fmt.Printf("       Remote Path: /var/www/oro/src/%s\n", config.GetBundlePath())
			fmt.Println("\nTo debug background processes, set in your .env:")
			fmt.Println("  - ORO_CONSUMER_XDEBUG_ENABLED=true (for message queue)")
			fmt.Println("  - ORO_CRON_XDEBUG_ENABLED=true (for cron jobs)")
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&cleanBeforeUp, "clean", "c", false, "Clean up environment before starting")
}
