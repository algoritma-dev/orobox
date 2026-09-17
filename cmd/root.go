// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Version is the current version of the tool.
var Version = "1.0.0-rc33"

var rootCmd = &cobra.Command{
	Use:     "orobox",
	Short:   "CLI tool for OroCommerce environment setup",
	Long:    `Orobox is a CLI tool to quickly configure an isolated development environment for OroCommerce bundles.`,
	Version: Version,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		// --debug wins. It exists to show everything, and silently discarding it would be a worse
		// surprise than ignoring the flag that asked for silence.
		//
		// cmd.Flags() rather than rootCmd.PersistentFlags(): cobra merges inherited flags into the
		// executing command, so this reads the value whether --agent came before or after the
		// subcommand name.
		agent, _ := cmd.Flags().GetBool("agent")
		output.SetAgent(agent && !viper.GetBool("debug"))

		if ConfigError != nil && !isConfigExempt(cmd) {
			utils.PrintError(ConfigError.Error())
			os.Exit(1)
		}

		// Started here rather than in Execute because agent mode is only known once the flags are
		// parsed, and a check that ignored it would print into a machine-readable stream.
		startUpdateCheck(cmd.Name())
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}

	// After the command, never before: the notice is an aside, and it would push the output the
	// user asked for further up the scrollback.
	printUpdateNotice()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .orobox.yaml)")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "show all docker output")
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	// Deliberately not bound to viper. Binding is what exposes a setting to AutomaticEnv under the
	// ORO_ prefix, and agent mode must never switch on because of an inherited environment: a
	// developer whose shell exported it once would get silent commands for the rest of the day.
	rootCmd.PersistentFlags().Bool("agent", false,
		"minimal output for automated callers: payload and errors only (ignored with --debug)")
}

// ConfigError contains the error if the configuration file is invalid.
var ConfigError error

// isConfigExempt reports whether a command may run without a valid .orobox.yaml.
// These commands either create the config/source tree or manage the binary itself.
func isConfigExempt(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "init", "self-update", "internal-gen-docker", "create":
		return true
	}
	// create's subcommands (project, bundle) run before any config exists.
	if cmd.Parent() != nil && cmd.Parent().Name() == "create" {
		return true
	}
	return false
}

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

		debug := viper.GetBool("debug")

		if ConfigError == nil && debug {
			utils.PrintInfo("Using config file: " + configFile)
		}
	}
}
