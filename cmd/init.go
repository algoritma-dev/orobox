// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/certificates"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	yamlv3 "gopkg.in/yaml.v3"
)

var (
	bundlePath      string
	oroVersion      string
	bundleNamespace string
	installType     string
	stdin           io.Reader = os.Stdin
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the development environment",
	Run: func(_ *cobra.Command, _ []string) {
		absPath, err := filepath.Abs(bundlePath)
		if err != nil {
			panic(err)
		}
		bundlePath = absPath

		err = os.MkdirAll(bundlePath, 0755)
		if err != nil {
			panic(err)
		}

		if err := os.Chdir(bundlePath); err != nil {
			panic(err)
		}

		generateConfig()

		// Reload config after generation
		viper.SetConfigFile(".orobox.yaml")
		if err := viper.ReadInConfig(); err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not read configuration: %v", err))
		}

		certificates.InstallSslCertificates()

		// Check hosts file
		var missingHosts []string
		for _, domain := range config.GetDomains() {
			if !utils.CheckHostInEtcHosts(domain.Host) {
				missingHosts = append(missingHosts, domain.Host)
			}
		}

		docker.EnsureDockerCompose()

		if !performInstallation() {
			return
		}

		utils.PrintSuccess("Environment initialized successfully!")

		if len(missingHosts) > 0 {
			utils.PrintTitle("Missing domains in hosts file")
			utils.PrintWarning("The following domains are missing from your hosts file. Please add them manually to /etc/hosts:")
			for _, host := range missingHosts {
				fmt.Printf("127.0.0.1 %s\n", host)
			}
		}
	},
}

func performInstallation() bool {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
		return false
	}

	// Remove any existing containers to ensure fresh bind mounts after init.
	// If vendor-oro was deleted and recreated, running containers would still hold
	// a bind mount to the old (deleted) inode, causing an empty vendor inside containers.
	if err := docker.RunComposeCommandSilently("Stopping existing containers...", "down", "--remove-orphans"); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not stop existing containers: %v", err))
	}

	// 0. Resolve OroCommerce version to the latest tag
	oroRepo := "https://github.com/oroinc/orocommerce-application.git"
	resolvedVersion, err := utils.GetLatestTag(oroRepo, conf.OroVersion)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not resolve latest tag for %s, using it as is: %v", conf.OroVersion, err))
		resolvedVersion = conf.OroVersion
	}

	// 1. Download sources (git clone)
	// (Project support removed from main branch)

	// 2. Ensure environment is ready.
	// gotenberg is always required by the install service.
	services := []string{"up", "-d", "db", "gotenberg"}
	if conf.Services.Redis {
		services = append(services, "redis")
	}
	if conf.Services.RabbitMQ {
		services = append(services, "rabbitmq")
	}
	if conf.Services.Elasticsearch {
		services = append(services, "elasticsearch")
	}
	if conf.Services.Mailpit {
		services = append(services, "mail")
	}
	if err := docker.RunComposeCommandSilently("Starting services for installation...", services...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
		return false
	}

	// Run volume-init to fix permissions before any composer/git command
	if err := docker.RunComposeCommandSilently("Ensuring permissions...", "run", "--rm", "-T", "volume-init"); err != nil {
		utils.PrintWarning(fmt.Sprintf("volume-init failed: %v", err))
	}

	// 3. For bundle, we need to clone into the volume if not already there
	// Always try to clone if composer.json is missing in the container
	checkCmd := []string{"run", "--rm", "-T", "application", "test", "-f", "composer.json"}
	utils.StartLoader("Checking for OroCommerce installation...")
	_, err = docker.RunComposeCommandWithOutput(checkCmd...)
	utils.StopLoader()
	if err != nil {
		// Use a temporary directory to clone, then move to avoid "directory not empty" errors if bundle is mounted
		cloneCmd := []string{"run", "--rm", "-T", "application", "bash", "-c",
			fmt.Sprintf("git clone -b %s --depth 1 %s /tmp/oro-app && cp -r /tmp/oro-app/. . && rm -rf /tmp/oro-app && composer install", resolvedVersion, oroRepo)}
		if err := docker.RunComposeCommandSilently("Downloading and installing OroCommerce into volume...", cloneCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("Download/Install into volume failed: %v", err))
			return false
		}
	} else {
		// Sources present: check for vendors (especially if vendor-oro was just added)
		checkVendor := []string{"run", "--rm", "-T", "application", "test", "-f", "vendor/autoload.php"}
		utils.StartLoader("Checking for vendors...")
		_, errVendor := docker.RunComposeCommandWithOutput(checkVendor...)
		utils.StopLoader()
		if errVendor != nil {
			installCmd := []string{"run", "--rm", "-T", "application", "composer", "install"}
			if err := docker.RunComposeCommandSilently("Installing dependencies...", installCmd...); err != nil {
				utils.PrintError(fmt.Sprintf("Composer install failed: %v", err))
				return false
			}
		}
	}

	// 4. Install bundle into vendor-oro via composer require (runs in 'application'
	// where vendor-oro is already mounted as the vendor directory).
	if bundlePackageName := getBundlePackageName(); bundlePackageName != "" {
		bundleNamespace := config.GetBundlePath()
		bashCmd := fmt.Sprintf(
			`COMPOSER_ALLOW_SUPERUSER=1 composer config repositories.bundle '{"type":"path","url":"bundles/%s","options":{"symlink":true}}'`,
			bundleNamespace,
		)
		for i, repo := range conf.Composer.Repositories {
			repoJSON, err := json.Marshal(repo)
			if err != nil {
				utils.PrintWarning(fmt.Sprintf("Could not serialize composer repository %d: %v", i, err))
				continue
			}
			bashCmd += fmt.Sprintf(
				` && COMPOSER_ALLOW_SUPERUSER=1 composer config repositories.orobox_%d '%s'`,
				i, string(repoJSON),
			)
		}
		bashCmd += fmt.Sprintf(
			` && COMPOSER_ALLOW_SUPERUSER=1 composer require "%s:@dev" --no-interaction --no-scripts`,
			bundlePackageName,
		)
		requireCmd := []string{"run", "--rm", "-T", "application", "bash", "-c", bashCmd}
		if err := docker.RunComposeCommandSilently("Installing bundle into vendor...", requireCmd...); err != nil {
			utils.PrintWarning(fmt.Sprintf("Bundle installation failed: %v", err))
		}
	}

	// 5. Run Oro installation.
	// We run volume-setup first for permissions, then install with --no-deps
	// because all dependencies (db, gotenberg, etc.) are already running above.
	// Using --no-deps avoids Docker Compose's dependency resolution, which can
	// trigger a "network not found" error when it tries to (re)start containers
	// whose network was replaced by the earlier `down --remove-orphans`.
	if err := docker.RunSetupComposeCommandSilently("Setting up volumes for installation...", "run", "--rm", "-T", "volume-setup"); err != nil {
		utils.PrintWarning(fmt.Sprintf("volume-setup failed: %v", err))
	}
	if err := docker.RunSetupComposeCommandSilently("Running OroCommerce installation (this may take several minutes)...", "run", "--rm", "-T", "--no-deps", "install"); err != nil {
		utils.PrintError(fmt.Sprintf("OroCommerce installation failed: %v", err))
		return false
	}

	return true
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVarP(&bundlePath, "bundle-path", "b", ".", "Bundle path")
	initCmd.Flags().StringVarP(&oroVersion, "oro-version", "v", "6.1", "OroCommerce version")
	initCmd.Flags().StringVarP(&bundleNamespace, "bundle-namespace", "n", "", "Bundle namespace")
	// Type flag removed from main branch as only 'bundle' is supported here
}

// getBundlePackageName reads the composer package name from the bundle's composer.json.
func getBundlePackageName() string {
	composerJSONPath := filepath.Join(config.GetHostBundlePath(), "composer.json")
	content, err := os.ReadFile(composerJSONPath)
	if err != nil {
		return ""
	}
	var data struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(content, &data); err != nil {
		return ""
	}
	return data.Name
}

func generateConfig() {
	configPath := ".orobox.yaml"
	if _, err := os.Stat(configPath); err == nil {
		// Config already exists, validate it
		if validateConfig() {
			return
		}
		utils.PrintWarning("Config file .orobox.yaml is invalid. Let's recreate it.")
	} else if !errors.Is(err, os.ErrNotExist) {
		utils.PrintWarning(fmt.Sprintf("Warning checking %s: %v", configPath, err))
		return
	}

	utils.PrintTitle("Config file .orobox.yaml not found or invalid. Let's create it interactively.")
	reader := bufio.NewReader(stdin)

	typeOfInstall := config.InstallTypeBundle

	var className, namespace string
	bundleClass := utils.AskQuestion(reader, "Full bundle class (eg: Algoritma\\Bundle\\TestBundle\\TestBundle)", "")

	if bundleClass != "" {
		var found bool
		className, namespace, _, found = config.FindPhpClass(".", bundleClass)
		if !found {
			utils.PrintWarning(fmt.Sprintf("PHP class for %s not found in current directory or subdirectories.", bundleClass))
			// Manual parsing if not found
			lastSlash := strings.LastIndex(bundleClass, "\\")
			if lastSlash != -1 {
				className = bundleClass[lastSlash+1:]
				namespace = bundleClass[:lastSlash]
			} else {
				className = bundleClass
				namespace = ""
			}
		} else {
			utils.PrintInfo(fmt.Sprintf("Found class %s in namespace %s", className, namespace))
		}
	}

	version := utils.AskSelection(reader, "OroCommerce version", config.SupportedOroVersions, oroVersion)
	host := utils.AskQuestion(reader, "Main domain host", "oro.demo")
	root := utils.AskQuestion(reader, "Main domain root", "public")
	ssl := utils.AskYesNo(reader, "Enable SSL?", true)

	redisEnabled := utils.AskYesNo(reader, "Enable Redis?", false)
	redisInsightEnabled := false
	if redisEnabled {
		redisInsightEnabled = utils.AskYesNo(reader, "Enable RedisInsight?", true)
	}

	mailpit := utils.AskYesNo(reader, "Enable Mailpit?", true)

	rabbitmqEnabled := utils.AskYesNo(reader, "Enable RabbitMQ?", false)
	elasticsearchEnabled := utils.AskYesNo(reader, "Enable Elasticsearch?", false)

	kibanaEnabled := false
	if elasticsearchEnabled {
		kibanaEnabled = utils.AskYesNo(reader, "Enable Kibana?", true)
	}

	adminerEnabled := utils.AskYesNo(reader, "Enable Adminer?", true)

	conf := config.OroConfig{
		Type:       typeOfInstall,
		Class:      className,
		Namespace:  namespace,
		OroVersion: version,
		Domains: []config.DomainConfig{
			{
				Host: host,
				Root: root,
				Ssl:  ssl,
			},
		},
		Services: config.ServicesConfig{
			Redis:         redisEnabled,
			RedisInsight:  redisInsightEnabled,
			Mailpit:       mailpit,
			RabbitMQ:      rabbitmqEnabled,
			Elasticsearch: elasticsearchEnabled,
			Kibana:        kibanaEnabled,
			Adminer:       adminerEnabled,
		},
		Test: config.TestConfig{
			UseTmpfs: false,
		},
	}

	data, err := yamlv3.Marshal(&conf)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Yaml marshal error: %s", err))
		return
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Write config error: %s", err))
	}
}

func validateConfig() bool {
	configPath := ".orobox.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	c, err := config.ParseConfig(data)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Validation error: %v", err))
		return false
	}
	if err := c.Validate(); err != nil {
		utils.PrintError(fmt.Sprintf("Validation error: %v", err))
		return false
	}
	return true
}
