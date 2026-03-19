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

type DomainConfig struct {
	Host string `yaml:"host" mapstructure:"host"`
	Root string `yaml:"root" mapstructure:"root"`
	Ssl  bool   `yaml:"ssl" mapstructure:"ssl"`
}

type ServicesConfig struct {
	Postgres      any    `yaml:"postgres" mapstructure:"postgres"`
	Redis         any    `yaml:"redis" mapstructure:"redis"`
	Mailpit       bool   `yaml:"mailpit" mapstructure:"mailpit"`
	PhpVersion    string `yaml:"php_version" mapstructure:"php_version"`
	NodeVersion   string `yaml:"node_version" mapstructure:"node_version"`
	NpmVersion    string `yaml:"npm_version" mapstructure:"npm_version"`
	RabbitMQ      any    `yaml:"rabbitmq" mapstructure:"rabbitmq"`
	Elasticsearch any    `yaml:"elasticsearch" mapstructure:"elasticsearch"`
}

type OroVersions struct {
	PHP           string
	Postgres      string
	Redis         string
	Node          string
	NPM           string
	RabbitMQ      string
	Elasticsearch string
}

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
			Node:          "20.10",
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
			NPM:           "10",
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

type OroConfig struct {
	Type       string         `yaml:"type" mapstructure:"type"`
	Class      string         `yaml:"class" mapstructure:"class"`
	Namespace  string         `yaml:"namespace" mapstructure:"namespace"`
	OroVersion string         `yaml:"oro_version" mapstructure:"oro_version"`
	Domains    []DomainConfig `yaml:"domains" mapstructure:"domains"`
	Services   ServicesConfig `yaml:"services" mapstructure:"services"`
}

const (
	OroRootDir       = "/var/www/oro"
	CustomBundlePath = "/src/CustomBundle"
)

func (c *OroConfig) Validate() error {
	if c.Namespace == "" {
		return errors.New("config error: field 'namespace' is required (did you use 'bundle_namespace' by mistake?)")
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

func ParseConfig(data []byte) (*OroConfig, error) {
	var c OroConfig
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func GetNamespace() string {
	ns := viper.GetString("namespace")
	if ns == "" {
		return "CustomBundle"
	}
	return ns
}

func GetBundlePath() string {
	ns := GetNamespace()
	return strings.ReplaceAll(ns, "\\", "/")
}

func GetHostBundlePath() string {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		// Fallback to current working directory
		dir, _ := os.Getwd()
		return dir
	}
	return filepath.Dir(configFile)
}

func GetProjectName() string {
	// The new config doesn't have "name", so we use the directory name
	currDir, _ := os.Getwd()
	return filepath.Base(currDir)
}

func GetInternalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to a relative path if home dir is not found
		return filepath.Join(".config", "orobox", GetProjectName())
	}
	return filepath.Join(home, ".config", "orobox", GetProjectName())
}

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
