// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
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
			fmt.Println("Configuration changed, rebuilding containers...")
			if err := docker.RunComposeCommand("build"); err != nil {
				fmt.Printf("Build failed: %v\n", err)
				return
			}
		}

		if cleanBeforeUp {
			fmt.Println("Cleaning up environment before starting...")
			if err := docker.RunComposeCommand("down", "-v", "--remove-orphans"); err != nil {
				fmt.Printf("Warning: failed to clean up: %v\n", err)
			}
		}

		fmt.Println("Environment started!")
		fmt.Println("Running OroCommerce bootstrap (this may take a few minutes)...")

		if err := docker.RunComposeCommand("run", "--rm", "restore"); err != nil {
			fmt.Printf("Bootstrap failed: %v\n", err)
			return
		}

		if err := docker.RunComposeCommand("up", "-d", "application"); err != nil {
			fmt.Printf("Bootstrap failed: %v\n", err)
			return
		}

		fmt.Println("\nOrobox is up and running!")

		for _, url := range docker.GetApplicationURLs() {
			fmt.Printf("- %s\n", url)
		}

		if viper.GetBool("services.mailpit") {
			fmt.Println("\nMailpit is available at:")
			fmt.Println("- http://localhost:8025")
		}

		if viper.GetBool("services.php.xdebug") {
			fmt.Println("\nXdebug is ENABLED.")
			fmt.Println("To debug in PhpStorm:")
			fmt.Println("1. Ensure the 'Phone' icon (Listener) is ON.")
			fmt.Println("2. Configure Path Mappings in Settings -> PHP -> Servers:")
			fmt.Println("   - Host: oro.demo (or your custom domain)")
			fmt.Printf("   - Local Path: %s\n", config.GetHostBundlePath())
			fmt.Printf("     Remote Path: /var/www/oro/src/%s\n", config.GetBundlePath())
			fmt.Println("\nTo debug background processes, set in your .env:")
			fmt.Println("   - ORO_CONSUMER_XDEBUG_ENABLED=true (for message queue)")
			fmt.Println("   - ORO_CRON_XDEBUG_ENABLED=true (for cron jobs)")
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&cleanBeforeUp, "clean", "c", false, "Clean up environment before starting")
}
