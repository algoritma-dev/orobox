package cmd

import (
	"fmt"
	"orobox/internal/config"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:     "oro",
	Short:   "CLI tool for OroCommerce environment setup",
	Long:    `Orobox is a CLI tool to quickly configure an isolated development environment for OroCommerce bundles.`,
	Version: "0.0.1-dev",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if ConfigError != nil && cmd.Name() != "init" {
			fmt.Fprintf(os.Stderr, "Error: %v\n", ConfigError)
			os.Exit(1)
		}
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .orobox.yaml)")
}

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
			fmt.Fprintln(os.Stderr, "Using config file:", configFile)
			fmt.Fprintln(os.Stderr, "Using box folder:", config.GetInternalDir())
		}
	}
}
