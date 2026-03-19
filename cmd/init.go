package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"orobox/internal/certificates"
	"orobox/internal/config"
	"orobox/internal/docker"
	"orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var bundlePath string
var oroVersion string
var bundleNamespace string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the development environment",
	Run: func(cmd *cobra.Command, args []string) {
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
		if err := viper.ReadInConfig(); err != nil {
			// Not a fatal error if it still doesn't exist
		}

		certificates.InstallSslCertificates()

		dockerfileIsChanged := docker.EnsureDockerCompose()

		if dockerfileIsChanged {
			if err := docker.RunComposeCommand("build"); err != nil {
				fmt.Printf("Build failed: %v\n", err)
				return
			}
		}

		fmt.Printf("Environment initialized successfully in current directory!\n")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVarP(&bundlePath, "bundle-path", "b", ".", "Bundle path")
	initCmd.Flags().StringVarP(&oroVersion, "oro-version", "v", "6.1", "OroCommerce version")
	initCmd.Flags().StringVarP(&bundleNamespace, "bundle-namespace", "n", "", "Bundle namespace")
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

	fmt.Println("Config file .orobox.yaml not found or invalid. Let's create it interactively.")
	reader := bufio.NewReader(os.Stdin)

	bundleClass := utils.AskQuestion(reader, "Full bundle class (eg: Algoritma\\Bundle\\ShippyProBundle\\AlgoritmaShippyProBundle)", "")

	var className, namespace string
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

	version := utils.AskQuestion(reader, "OroCommerce version", oroVersion)
	host := utils.AskQuestion(reader, "Main domain host", "localhost")
	root := utils.AskQuestion(reader, "Main domain root", "public")
	ssl := utils.AskYesNo(reader, "Enable SSL?", false)
	redisEnabled := utils.AskYesNo(reader, "Enable Redis?", true)
	mailpit := utils.AskYesNo(reader, "Enable Mailpit?", true)
	rabbitmqEnabled := utils.AskYesNo(reader, "Enable RabbitMQ?", true)
	elasticsearchEnabled := utils.AskYesNo(reader, "Enable Elasticsearch?", true)

	versions := config.GetVersionsForOro(version)

	var redis any = false
	if redisEnabled {
		redis = versions.Redis
	}
	var rabbitmq any = false
	if rabbitmqEnabled {
		rabbitmq = versions.RabbitMQ
	}
	var elasticsearch any = false
	if elasticsearchEnabled {
		elasticsearch = versions.Elasticsearch
	}

	conf := config.OroConfig{
		Type:       "bundle",
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
			Postgres:      versions.Postgres,
			Redis:         redis,
			Mailpit:       mailpit,
			PhpVersion:    versions.PHP,
			NodeVersion:   versions.Node,
			NpmVersion:    versions.NPM,
			RabbitMQ:      rabbitmq,
			Elasticsearch: elasticsearch,
		},
	}

	data, err := yaml.Marshal(&conf)
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
