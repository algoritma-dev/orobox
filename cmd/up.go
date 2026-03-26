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

		if err := docker.RunComposeCommandSilently("Starting containers...", "up", "-d"); err != nil {
			utils.PrintError(fmt.Sprintf("Startup failed: %v", err))
			return
		}

		fmt.Println()
		utils.PrintSuccess("Orobox is up and running!")

		var urls = docker.GetApplicationURLs()

		utils.PrintTitle("The application is available at:")

		if len(urls) > 0 {
			fmt.Printf("Backoffice: %s/admin (admin/admin)\n", urls[0])
			fmt.Println("Storefront:")

			for _, url := range urls {
				fmt.Printf("  - %s\n", url)
			}
		} else {
			fmt.Println("No application URLs configured. Set at least one domain in your config.")
		}

		if viper.GetBool("services.mailpit") {
			utils.PrintTitle("Mailpit is available at:")
			fmt.Println("  - http://localhost:8025")
			fmt.Printf("  - Set in your .env:\n")
			fmt.Printf("	- ORO_MAILER_DSN=smtp://mail:1025\n")
		}

		if viper.GetBool("services.redis") {
			utils.PrintTitle("Redis is available at:")
			fmt.Printf("  - RedisInsight UI: http://localhost:8001\n")
			fmt.Printf("  - Set in your .env:\n")
			fmt.Printf("	- ORO_REDIS_URL=redis://redis:6379\n")
		}

		if viper.GetBool("services.rabbitmq") {
			utils.PrintTitle("RabbitMQ is available at:")
			fmt.Printf("  - Management UI: http://localhost:15672 (guest/guest)\n")
			fmt.Printf("  - Set in your .env:\n")
			fmt.Printf("	- MESSENGER_TRANSPORT_DSN=amqp://guest:guest@rabbitmq:5672/%%2f/messages\n")
		}

		if viper.GetBool("services.elasticsearch") {
			utils.PrintTitle("Elasticsearch is available at:")
			fmt.Printf("  - Kibana UI: http://localhost:5601\n")
			fmt.Printf("  - Set in your .env:\n")
			fmt.Printf("	- ORO_SEARCH_URL=http://elasticsearch:9200\n")
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
