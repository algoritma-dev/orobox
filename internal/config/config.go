// Package config provides configuration management for Orobox.
package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	yamlv3 "gopkg.in/yaml.v3"
)

// DomainConfig represents the configuration for a domain.
type DomainConfig struct {
	Host string `yaml:"host" mapstructure:"host"`
	Root string `yaml:"root" mapstructure:"root"`
	Ssl  bool   `yaml:"ssl" mapstructure:"ssl"`
}

// ServicesConfig represents the configuration for various services.
type ServicesConfig struct {
	Redis         bool `yaml:"redis" mapstructure:"redis"`
	Mailpit       bool `yaml:"mailpit" mapstructure:"mailpit"`
	RabbitMQ      bool `yaml:"rabbitmq" mapstructure:"rabbitmq"`
	Elasticsearch bool `yaml:"elasticsearch" mapstructure:"elasticsearch"`
	RedisInsight  bool `yaml:"redisinsight" mapstructure:"redisinsight"`
	Kibana        bool `yaml:"kibana" mapstructure:"kibana"`
	Adminer       bool `yaml:"adminer" mapstructure:"adminer"`
}

// TestConfig represents the configuration for the test environment.
type TestConfig struct {
	UseTmpfs  bool   `yaml:"use_tmpfs" mapstructure:"use_tmpfs"`
	TmpfsSize string `yaml:"tmpfs_size" mapstructure:"tmpfs_size"`
}

// CommandConfig represents a custom command that can be run in the container.
type CommandConfig struct {
	Name        string `yaml:"name" mapstructure:"name"`
	Command     string `yaml:"command" mapstructure:"command"`
	Description string `yaml:"description" mapstructure:"description"`
	Service     string `yaml:"service" mapstructure:"service"`
}

// OroVersions defines the versions of components for a specific OroCommerce version.
type OroVersions struct {
	PHP           string
	Postgres      string
	Redis         string
	Node          string
	NPM           string
	RabbitMQ      string
	Elasticsearch string
}

// SupportedOroVersions is the list of supported OroCommerce versions.
var SupportedOroVersions = []string{"6.1", "6.0", "5.1"}

// GetVersionsForOro returns the component versions for a given OroCommerce version.
func GetVersionsForOro(oroVersion string) OroVersions {
	switch oroVersion {
	case "7.0":
		return OroVersions{
			PHP:           "8.5",
			Postgres:      "17.6-alpine",
			Redis:         "7.4-alpine",
			Node:          "22",
			NPM:           "10",
			RabbitMQ:      "4.2-management-alpine",
			Elasticsearch: "9.2.0",
		}
	case "6.1":
		return OroVersions{
			PHP:           "8.4",
			Postgres:      "16.1-alpine",
			Redis:         "7.2-alpine",
			Node:          "22",
			NPM:           "10",
			RabbitMQ:      "3.12-management-alpine",
			Elasticsearch: "8.4.1",
		}
	case "6.0":
		return OroVersions{
			PHP:           "8.3",
			Postgres:      "16.1-alpine",
			Redis:         "7.0-alpine",
			Node:          "20.19",
			NPM:           "10",
			RabbitMQ:      "3.12-management-alpine",
			Elasticsearch: "8.4.1",
		}
	case "5.1":
		return OroVersions{
			PHP:           "8.2",
			Postgres:      "16.1-alpine",
			Redis:         "6.2-alpine",
			Node:          "18.14",
			NPM:           "9.3",
			RabbitMQ:      "3.11-management-alpine",
			Elasticsearch: "8.4.1",
		}
	default:
		// Fallback for other versions or default
		if oroVersion >= "7.0" {
			return GetVersionsForOro("7.0")
		}
		if oroVersion >= "6.1" {
			return GetVersionsForOro("6.1")
		}
		if oroVersion >= "6.0" {
			return GetVersionsForOro("6.0")
		}
		return GetVersionsForOro("5.1")
	}
}

// OroConfig is the main configuration structure for Orobox.
type OroConfig struct {
	Type       string          `yaml:"type" mapstructure:"type" default:"bundle"`
	Class      string          `yaml:"class" mapstructure:"class"`
	Namespace  string          `yaml:"namespace" mapstructure:"namespace"`
	OroVersion string          `yaml:"oro_version" mapstructure:"oro_version"`
	Domains    []DomainConfig  `yaml:"domains" mapstructure:"domains"`
	Services   ServicesConfig  `yaml:"services" mapstructure:"services"`
	Test       TestConfig      `yaml:"test" mapstructure:"test"`
	Commands   []CommandConfig `yaml:"commands" mapstructure:"commands"`
}

// Install types for OroCommerce.
const (
	InstallTypeBundle  = "bundle"
	InstallTypeProject = "project"
)

// OroRootDir is the base directory for OroCommerce in the container.
const OroRootDir = "/var/www/oro"

// CustomBundlePath is the base path for custom bundles.
const CustomBundlePath = "/src/CustomBundle"

// Validate checks if the configuration is valid.
func (c *OroConfig) Validate() error {
	if c.Type == "" {
		c.Type = InstallTypeBundle
	}

	if c.Type == InstallTypeBundle {
		if c.Namespace == "" {
			return errors.New("config error: field 'namespace' is required (did you use 'bundle_namespace' by mistake?)")
		}
	}

	if c.OroVersion == "" {
		return errors.New("config error: field 'oro_version' is required")
	}
	if len(c.Domains) == 0 {
		return errors.New("config error: at least one domain must be configured")
	}
	for i, domain := range c.Domains {
		if domain.Host == "" {
			return errors.New("config error: 'host' is required for domain at index " + string(rune(i)))
		}
	}
	return nil
}

// ParseConfig parses a configuration from bytes.
func ParseConfig(data []byte) (*OroConfig, error) {
	var c OroConfig
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConfig saves the configuration to the specified path.
func SaveConfig(path string, c *OroConfig) error {
	data, err := yamlv3.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// GetNamespace returns the project namespace.
func GetNamespace() string {
	ns := viper.GetString("namespace")
	if ns == "" {
		return "CustomBundle"
	}
	return ns
}

// GetBundlePath returns the relative path to the bundle.
func GetBundlePath() string {
	ns := GetNamespace()
	return strings.ReplaceAll(ns, "\\", "/")
}

// GetHostBundlePath returns the absolute path to the bundle on the host.
func GetHostBundlePath() string {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		// Fallback to current working directory
		dir, _ := os.Getwd()
		return dir
	}
	return filepath.Dir(configFile)
}

// GetProjectName returns the name of the current project.
func GetProjectName() string {
	// The new config doesn't have "name", so we use the directory name
	currDir, _ := os.Getwd()
	return filepath.Base(currDir)
}

// GetInternalDir returns the internal directory for storing Orobox data.
func GetInternalDir() string {
	if os.Getenv("CI") != "" || os.Getenv("OROBOX_LOCAL_CONFIG") != "" {
		return ".orobox"
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return ".orobox"
	}

	projectName := GetProjectName()
	return filepath.Join(configDir, "orobox", projectName)
}

// GetFirstDomainHost returns the host of the first configured domain.
func GetFirstDomainHost() string {
	domains := GetDomains()
	if len(domains) > 0 {
		return domains[0].Host
	}
	return "oro.demo"
}

// GetDomains returns the list of configured domains.
func GetDomains() []DomainConfig {
	var domains []DomainConfig
	_ = viper.UnmarshalKey("domains", &domains)
	return domains
}

// FindPhpClass tries to find a PHP class in the project directory.
func FindPhpClass(className string) (string, string, bool) {
	// If the user provides a full namespace like Algoritma\Bundle\ShippyProBundle\AlgoritmaShippyProBundle
	parts := strings.Split(className, "\\")
	shortClassName := parts[len(parts)-1]
	namespace := strings.Join(parts[:len(parts)-1], "\\")

	foundPath := ""
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == shortClassName+".php" {
			foundPath = path
			return filepath.SkipDir // optimization? no, we want to find one.
		}
		return nil
	})

	if err == nil && foundPath != "" {
		return shortClassName, namespace, true
	}

	return "", "", false
}
