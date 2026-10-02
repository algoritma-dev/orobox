// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/certificates"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"

	"github.com/spf13/cobra"
)

var (
	cleanBeforeUp bool
	rebuildImage  bool
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the development environment",
	// A stack that will not start is a runtime problem, not a usage problem: the flag list
	// printed after it buries the compose output that says what actually went wrong.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		certificates.InstallSslCertificates()
		docker.SetForceCustomImageRebuild(rebuildImage)
		configChanged := docker.EnsureDockerCompose()

		if cleanBeforeUp {
			if err := docker.RunComposeCommandSilently("Cleaning up environment...", "down", "-v", "--remove-orphans"); err != nil {
				utils.PrintWarning(fmt.Sprintf("failed to clean up: %v", err))
			}
		}

		// Compose builds a `build:` service only when its image is missing; --build makes an
		// edited Dockerfile in the override take effect on the next `orobox up`.
		upArgs := []string{"up", "-d"}
		if docker.OverrideAnalysis().HasBuild {
			upArgs = append(upArgs, "--build")
		}
		if err := docker.RunComposeCommandSilently("Starting containers...", upArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Startup failed: %v", err))
			return err
		}

		// A container that was already running keeps its bind-mounted nginx.conf, so a
		// regenerated configuration (new domain, websocket proxy, ...) needs a reload.
		if configChanged {
			if err := docker.ReloadWebServer(); err != nil {
				utils.PrintWarning(fmt.Sprintf("Could not reload the web server configuration: %v", err))
			}
		}

		utils.PrintPlain("")
		utils.PrintSuccess("Orobox is up and running!")

		var urls = docker.GetApplicationURLs()

		utils.PrintTitle("The application is available at:")

		if len(urls) > 0 {
			utils.PrintPlainf("Backoffice: %s/admin (admin/admin)\n", urls[0])
			utils.PrintPlain("Storefront:")

			for _, url := range urls {
				utils.PrintPlainf("  - %s\n", url)
			}
		} else {
			utils.PrintPlain("No application URLs configured. Set at least one domain in your config.")
		}

		if viper.GetBool("services.mailpit") {
			utils.PrintTitle("Mailpit is available at:")
			printLocalURL("", "mail_ui", "")
			utils.PrintPlainf("  - Set in your .env:\n")
			utils.PrintPlainf("	- ORO_MAILER_DSN=smtp://mail:1025\n")
		}

		dbUser, dbPass, dbName, _ := docker.GetDatabaseCredentials()

		// With Adminer unpublished the block would be a header and credentials for a UI that
		// cannot be opened, so the whole block goes.
		if serviceEnabled("services.adminer", true) && config.GetPort("adminer") != 0 {
			utils.PrintTitle("Adminer is available at:")
			printLocalURL("", "adminer", "")
			utils.PrintPlainf("  - Credentials: %s / %s (Database: %s)\n", dbUser, dbPass, dbName)
		}

		// The database always runs; with its port unpublished there is nothing to connect to
		// from the host.
		if dbPort := config.GetPort("db"); dbPort != 0 {
			utils.PrintTitle("External Database Connection (e.g. PhpStorm):")
			utils.PrintPlain("  - Host: localhost")
			utils.PrintPlainf("  - Port: %d\n", dbPort)
			utils.PrintPlainf("  - User: %s\n", dbUser)
			utils.PrintPlainf("  - Password: %s\n", dbPass)
			utils.PrintPlainf("  - Database: %s\n", dbName)
		}

		if viper.GetBool("services.redis") {
			utils.PrintTitle("Redis is available at:")
			if serviceEnabled("services.redisinsight", true) {
				printLocalURL("RedisInsight UI: ", "redisinsight", "")
			}
			utils.PrintPlainf("  - Set in your .env:\n")
			utils.PrintPlainf("	- ORO_REDIS_URL=redis://redis:6379\n")
		}

		if viper.GetBool("services.rabbitmq") {
			utils.PrintTitle("RabbitMQ is available at:")
			printLocalURL("Management UI: ", "rabbitmq_ui", " (guest/guest)")
			utils.PrintPlainf("  - Set in your .env:\n")
			utils.PrintPlainf("	- MESSENGER_TRANSPORT_DSN=amqp://guest:guest@rabbitmq:5672/%%2f/messages\n")
		}

		if viper.GetBool("services.elasticsearch") {
			utils.PrintTitle("Elasticsearch is available at:")
			if serviceEnabled("services.kibana", true) {
				printLocalURL("Kibana UI: ", "kibana", "")
			}
			utils.PrintPlainf("  - Set in your .env:\n")
			utils.PrintPlainf("	- ORO_SEARCH_URL=http://elasticsearch:9200\n")
		}

		// Last, after the built-in blocks: these are the user's own services, advertised
		// through the dev.orobox.url label in their compose override.
		if projectURLs := docker.OverrideAnalysis().URLs; len(projectURLs) > 0 {
			utils.PrintTitle("Project services:")
			for _, u := range projectURLs {
				utils.PrintPlainf("  - %s: %s\n", u.Service, u.URL)
			}
		}

		return nil
	},
}

// serviceEnabled reads an optional-service toggle the way the compose generation does: unset
// means the default (Adminer on with PostgreSQL, RedisInsight with Redis, Kibana with
// Elasticsearch), so up never advertises a service compose does not run, or hides one it does.
func serviceEnabled(key string, def bool) bool {
	if !viper.IsSet(key) {
		return def
	}
	return viper.GetBool(key)
}

// printLocalURL advertises a service published on the host as a "  - <label>http://localhost:<port><suffix>"
// line. The port comes from the `ports:` config so the line matches what compose really
// published; a port set to 0 is not published, and a URL that cannot be opened is not printed.
func printLocalURL(label, portKey, suffix string) {
	port := config.GetPort(portKey)
	if port == 0 {
		return
	}
	utils.PrintPlainf("  - %shttp://localhost:%d%s\n", label, port, suffix)
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&cleanBeforeUp, "clean", "c", false, "Clean up environment before starting")
	upCmd.Flags().BoolVar(&rebuildImage, "rebuild", false, "Rebuild the project's image layer (image.* keys and/or image.dockerfile) ignoring the Docker cache")
}
