// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Version is the current version of the tool.
var Version = "0.0.7-dev"

var rootCmd = &cobra.Command{
	Use:     "oro",
	Short:   "CLI tool for OroCommerce environment setup",
	Long:    `Orobox is a CLI tool to quickly configure an isolated development environment for OroCommerce bundles.`,
	Version: Version,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		if ConfigError != nil && cmd.Name() != "init" {
			utils.PrintError(ConfigError.Error())
			os.Exit(1)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .orobox.yaml)")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "mostra tutto l'output di docker")
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
}

// ConfigError contains the error if the configuration file is invalid.
var ConfigError error

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".orobox")
	}

	viper.SetEnvPrefix("ORO")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		configFile := viper.ConfigFileUsed()
		data, err := os.ReadFile(configFile)
		if err == nil {
			c, err := config.ParseConfig(data)
			if err != nil {
				ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
			} else if err := c.Validate(); err != nil {
				ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
			}
		}

		if ConfigError == nil {
			utils.PrintInfo("Using config file: " + configFile)
		}
	}
}
