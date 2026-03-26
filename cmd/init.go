// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
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
	nonInteractive  bool
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

		if nonInteractive {
			os.Setenv("OROBOX_NON_INTERACTIVE", "1")
			viper.Set("type", installType)
			viper.Set("oro_version", oroVersion)
		}

		generateConfig()

		// Reload config after generation
		viper.SetConfigFile(".orobox.yaml")
		_ = viper.ReadInConfig()

		certificates.InstallSslCertificates()

		dockerfileIsChanged := docker.EnsureDockerCompose()

		if dockerfileIsChanged {
			fmt.Println("Building Docker images...")
			if err := docker.RunComposeCommand("build"); err != nil {
				fmt.Printf("Build failed: %v\n", err)
				return
			}
		}

		if !performInstallation() {
			return
		}

		fmt.Printf("Environment initialized successfully!\n")
	},
}

func performInstallation() bool {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		return false
	}

	// 1. Download sources (git clone)
	if conf.Type == config.InstallTypeProject {
		if _, err := os.Stat("composer.json"); os.IsNotExist(err) {
			fmt.Printf("Cloning OroCommerce %s...\n", conf.OroVersion)
			// Use a temporary directory to clone, then move to avoid "directory not empty" errors (like .orobox.yaml)
			tmpDir, err := os.MkdirTemp("", "oro-app-*")
			if err != nil {
				fmt.Printf("Temp dir creation failed: %v\n", err)
				return false
			}
			defer os.RemoveAll(tmpDir)

			cloneCmd := exec.Command("git", "clone", "-b", conf.OroVersion, "https://github.com/oroinc/orocommerce-application.git", tmpDir)
			cloneCmd.Stdout = os.Stdout
			cloneCmd.Stderr = os.Stderr
			if err := cloneCmd.Run(); err != nil {
				fmt.Printf("Clone failed: %v\n", err)
				return false
			}

			fmt.Println("Extracting OroCommerce sources...")
			// Use cp -r to merge directories and copy hidden files
			cpCmd := exec.Command("cp", "-r", tmpDir+"/.", ".")
			if err := cpCmd.Run(); err != nil {
				fmt.Printf("Extract sources failed: %v\n", err)
				return false
			}
		}
	}

	// 2. Ensure environment is ready
	fmt.Println("Starting services for installation...")
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
	if err := docker.RunComposeCommand(services...); err != nil {
		fmt.Printf("Failed to start services: %v\n", err)
		return false
	}

	// Run volume-init to fix permissions before any composer/git command
	fmt.Println("Ensuring permissions...")
	if err := docker.RunComposeCommand("run", "--rm", "volume-init"); err != nil {
		fmt.Printf("Warning: volume-init failed: %v\n", err)
	}

	// 3. For bundle or demo, we might need to clone into the volume if not project
	if conf.Type != config.InstallTypeProject {
		fmt.Println("Preparing OroCommerce in volume...")
		// Always try to clone if composer.json is missing in the container
		checkCmd := []string{"run", "--rm", "application", "ls", "composer.json"}
		if err := docker.RunComposeCommand(checkCmd...); err != nil {
			fmt.Println("Downloading OroCommerce into volume...")
			// Use a temporary directory to clone, then move to avoid "directory not empty" errors if bundle is mounted
			cloneCmd := []string{"run", "--rm", "application", "bash", "-c",
				fmt.Sprintf("git clone -b %s --depth 1 https://github.com/oroinc/orocommerce-application /tmp/oro-app && cp -r /tmp/oro-app/. . && rm -rf /tmp/oro-app && composer install", conf.OroVersion)}
			if err := docker.RunComposeCommand(cloneCmd...); err != nil {
				fmt.Printf("Download/Install into volume failed: %v\n", err)
				return false
			}
		}
	} else {
		// Project mode: just composer install
		fmt.Println("Running composer install...")
		if err := docker.RunComposeCommand("run", "--rm", "application", "composer", "install"); err != nil {
			fmt.Printf("Composer install failed: %v\n", err)
			return false
		}
	}

	// 4. Run Oro installation
	fmt.Println("Running OroCommerce installation (this may take several minutes)...")
	// Use the 'install' service from docker-compose.setup.yml
	if err := docker.RunComposeCommand("run", "--rm", "install"); err != nil {
		fmt.Printf("OroCommerce installation failed: %v\n", err)
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
	initCmd.Flags().BoolVarP(&nonInteractive, "non-interactive", "i", false, "Run in non-interactive mode")
}

func generateConfig() {
	configPath := ".orobox.yaml"
	if _, err := os.Stat(configPath); err == nil {
		// Config already exists, validate it
		if validateConfig() {
			return
		}
		fmt.Println("Config file .orobox.yaml is invalid. Let's recreate it.")
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("Warning checking %s: %v\n", configPath, err)
		return
	}

	fmt.Println("Config file .orobox.yaml not found or invalid. Let's create it.")

	if nonInteractive {
		var className, namespace string
		if installType == config.InstallTypeBundle {
			if bundleNamespace == "" {
				fmt.Println("Error: bundle-namespace is required for bundle installation type in non-interactive mode")
				os.Exit(1)
			}
			var found bool
			className, namespace, found = config.FindPhpClass(bundleNamespace)
			if !found {
				lastSlash := strings.LastIndex(bundleNamespace, "\\")
				if lastSlash != -1 {
					className = bundleNamespace[lastSlash+1:]
					namespace = bundleNamespace[:lastSlash]
				} else {
					className = bundleNamespace
					namespace = ""
				}
			}
		}

		conf := config.OroConfig{
			Type:       installType,
			Class:      className,
			Namespace:  namespace,
			OroVersion: oroVersion,
			Domains: []config.DomainConfig{
				{
					Host: "oro.demo",
					Root: "public",
					Ssl:  true,
				},
			},
			Services: config.ServicesConfig{
				Redis:   false,
				Mailpit: installType != config.InstallTypeDemo,
				Php: config.PhpConfig{
					Xdebug: false,
				},
				RabbitMQ:      false,
				Elasticsearch: false,
			},
		}

		data, err := yamlv3.Marshal(&conf)
		if err != nil {
			fmt.Printf("Warning: %s\n", err)
			return
		}

		err = os.WriteFile(configPath, data, 0644)
		if err != nil {
			fmt.Printf("Warning: %s\n", err)
		}
		return
	}

	fmt.Println("Let's create it interactively.")
	reader := bufio.NewReader(stdin)

	typeOfInstall := utils.AskSelection(reader, "Installation type", []string{config.InstallTypeBundle, config.InstallTypeProject, config.InstallTypeDemo}, installType)

	var className, namespace string
	if typeOfInstall == config.InstallTypeBundle {
		bundleClass := utils.AskQuestion(reader, "Full bundle class (eg: Algoritma\\Bundle\\ShippyProBundle\\AlgoritmaShippyProBundle)", "")

		if bundleClass != "" {
			var found bool
			className, namespace, found = config.FindPhpClass(bundleClass)
			if !found {
				fmt.Printf("Warning: PHP class for %s not found in current directory or subdirectories.\n", bundleClass)
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
				fmt.Printf("Found class %s in namespace %s\n", className, namespace)
			}
		}
	}

	version := utils.AskSelection(reader, "OroCommerce version", config.SupportedOroVersions, oroVersion)
	host := utils.AskQuestion(reader, "Main domain host", "oro.demo")
	root := utils.AskQuestion(reader, "Main domain root", "public")
	ssl := utils.AskYesNo(reader, "Enable SSL?", true)

	isDemo := typeOfInstall == config.InstallTypeDemo

	redisEnabled := utils.AskYesNo(reader, "Enable Redis?", false)
	mailpit := false
	if !isDemo {
		mailpit = utils.AskYesNo(reader, "Enable Mailpit?", true)
	}
	rabbitmqEnabled := utils.AskYesNo(reader, "Enable RabbitMQ?", false)
	elasticsearchEnabled := utils.AskYesNo(reader, "Enable Elasticsearch?", false)

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
			Redis:   redisEnabled,
			Mailpit: mailpit,
			Php: config.PhpConfig{
				Xdebug: false,
			},
			RabbitMQ:      rabbitmqEnabled,
			Elasticsearch: elasticsearchEnabled,
		},
	}

	data, err := yamlv3.Marshal(&conf)
	if err != nil {
		fmt.Printf("Warning: %s\n", err)
		return
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		fmt.Printf("Warning: %s\n", err)
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
		fmt.Printf("Validation error: %v\n", err)
		return false
	}
	if err := c.Validate(); err != nil {
		fmt.Printf("Validation error: %v\n", err)
		return false
	}
	return true
}
