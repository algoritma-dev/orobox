// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
		_ = viper.ReadInConfig()

		certificates.InstallSslCertificates()

		docker.EnsureDockerCompose()

		if !performInstallation() {
			return
		}

		utils.PrintSuccess("Environment initialized successfully!")
	},
}

func performInstallation() bool {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
		return false
	}

	// 0. Resolve OroCommerce version to the latest tag
	oroRepo := "https://github.com/oroinc/orocommerce-application.git"
	resolvedVersion, err := utils.GetLatestTag(oroRepo, conf.OroVersion)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not resolve latest tag for %s, using it as is: %v", conf.OroVersion, err))
		resolvedVersion = conf.OroVersion
	}

	// 1. Download sources (git clone)
	if conf.Type == config.InstallTypeProject {
		if _, err := os.Stat("composer.json"); os.IsNotExist(err) {
			utils.PrintInfo(fmt.Sprintf("Cloning OroCommerce %s...", resolvedVersion))
			// Use a temporary directory to clone, then move to avoid "directory not empty" errors (like .orobox.yaml)
			tmpDir, err := os.MkdirTemp("", "oro-app-*")
			if err != nil {
				utils.PrintError(fmt.Sprintf("Temp dir creation failed: %v", err))
				return false
			}
			defer os.RemoveAll(tmpDir)

			cloneCmd := exec.Command("git", "clone", "-b", resolvedVersion, oroRepo, tmpDir)
			// Hidden output for git clone as well, unless error
			var stderr bytes.Buffer
			cloneCmd.Stderr = &stderr
			if err := cloneCmd.Run(); err != nil {
				utils.PrintError(fmt.Sprintf("Clone failed: %v\n%s", err, stderr.String()))
				return false
			}

			utils.PrintInfo("Extracting OroCommerce sources...")
			// Use cp -r to merge directories and copy hidden files
			cpCmd := exec.Command("cp", "-r", tmpDir+"/.", ".")
			if err := cpCmd.Run(); err != nil {
				utils.PrintError(fmt.Sprintf("Extract sources failed: %v", err))
				return false
			}
		}
	}

	// 2. Ensure environment is ready
	services := []string{"up", "-d", "db"}
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

	// 3. For bundle or demo, we might need to clone into the volume if not project
	if conf.Type != config.InstallTypeProject {
		// Always try to clone if composer.json is missing in the container
		checkCmd := []string{"run", "--rm", "-T", "application", "test", "-f", "composer.json"}
		utils.StartLoader("Checking for OroCommerce installation...")
		_, err := docker.RunComposeCommandWithOutput(checkCmd...)
		utils.StopLoader()
		if err != nil {
			// Use a temporary directory to clone, then move to avoid "directory not empty" errors if bundle is mounted
			cloneCmd := []string{"run", "--rm", "-T", "application", "bash", "-c",
				fmt.Sprintf("git clone -b %s --depth 1 %s /tmp/oro-app && cp -r /tmp/oro-app/. . && rm -rf /tmp/oro-app && composer install", resolvedVersion, oroRepo)}
			if err := docker.RunComposeCommandSilently("Downloading and installing OroCommerce into volume...", cloneCmd...); err != nil {
				utils.PrintError(fmt.Sprintf("Download/Install into volume failed: %v", err))
				return false
			}
		}
	} else {
		// Project mode: just composer install
		if err := docker.RunComposeCommandSilently("Running composer install...", "run", "--rm", "-T", "application", "composer", "install"); err != nil {
			utils.PrintError(fmt.Sprintf("Composer install failed: %v", err))
			return false
		}
	}

	// 4. Run Oro installation
	// Use the 'install' service from docker-compose.setup.yml
	if err := docker.RunComposeCommandSilently("Running OroCommerce installation (this may take several minutes)...", "run", "--rm", "-T", "install"); err != nil {
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
	initCmd.Flags().StringVarP(&installType, "type", "t", "bundle", "Installation type (bundle, project, demo)")
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

	typeOfInstall := utils.AskSelection(reader, "Installation type", []string{config.InstallTypeBundle, config.InstallTypeProject, config.InstallTypeDemo}, installType)

	var className, namespace string
	if typeOfInstall == config.InstallTypeBundle {
		bundleClass := utils.AskQuestion(reader, "Full bundle class (eg: Algoritma\\Bundle\\TestBundle\\TestBundle)", "")

		if bundleClass != "" {
			var found bool
			className, namespace, found = config.FindPhpClass(bundleClass)
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
	}

	version := utils.AskSelection(reader, "OroCommerce version", config.SupportedOroVersions, oroVersion)
	host := utils.AskQuestion(reader, "Main domain host", "oro.demo")
	root := utils.AskQuestion(reader, "Main domain root", "public")
	ssl := utils.AskYesNo(reader, "Enable SSL?", true)

	isDemo := typeOfInstall == config.InstallTypeDemo

	redisEnabled := utils.AskYesNo(reader, "Enable Redis?", false)
	redisInsightEnabled := false
	if redisEnabled && !isDemo {
		redisInsightEnabled = utils.AskYesNo(reader, "Enable RedisInsight?", true)
	}

	mailpit := false
	if !isDemo {
		mailpit = utils.AskYesNo(reader, "Enable Mailpit?", true)
	}

	rabbitmqEnabled := utils.AskYesNo(reader, "Enable RabbitMQ?", false)
	elasticsearchEnabled := utils.AskYesNo(reader, "Enable Elasticsearch?", false)

	kibanaEnabled := false
	if elasticsearchEnabled && !isDemo {
		kibanaEnabled = utils.AskYesNo(reader, "Enable Kibana?", true)
	}

	adminerEnabled := false
	if !isDemo {
		adminerEnabled = utils.AskYesNo(reader, "Enable Adminer?", true)
	}

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
			Redis:        redisEnabled,
			RedisInsight: redisInsightEnabled,
			Mailpit:      mailpit,
			Php: config.PhpConfig{
				Xdebug: false,
			},
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
