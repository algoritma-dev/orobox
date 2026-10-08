// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Version is the current version of the tool.
var Version = "1.3.1"

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

		if err := configGate(cmd); err != nil {
			utils.PrintError(err.Error())
			exitOnConfigError(1)
			return
		}

		warnDeprecatedConfig(cmd)

		// Started here rather than in Execute because agent mode is only known once the flags are
		// parsed, and a check that ignored it would print into a machine-readable stream.
		startUpdateCheck(cmd.Name())
	},
}

// warnDeprecatedConfig tells the user to move the old top-level `dockerfile` key under `image:`.
//
// It runs here and not in initConfig for two reasons: only a config that passed Validate should
// be commented on, and agent mode is only known once PersistentPreRun has set it — the helper
// drops the message in agent mode. It goes to stderr because it is printed before every command,
// whose stdout may be piped or captured, and it is skipped for the commands whose output is
// consumed verbatim (see printsVerbatimOutput).
func warnDeprecatedConfig(cmd *cobra.Command) {
	if ConfigError != nil || !config.DeprecatedDockerfileKeyUsed() || printsVerbatimOutput(cmd) {
		return
	}
	utils.PrintWarningStderr("The top-level 'dockerfile' key is deprecated: move it under 'image:' as 'image.dockerfile'.")
}

// printsVerbatimOutput reports whether cmd, or a command it belongs to, produces output that is
// read as is rather than by a person scanning it: `completion <shell>` writes a script the shell
// sources, cobra's hidden `__complete` commands answer the shell's completion requests on every
// TAB, and `help` / `version` are what a user runs to find out how to fix the config. A notice
// about the config does not belong in any of them, not even on stderr: during completion stderr
// is the terminal the user is typing in.
func printsVerbatimOutput(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd, "help", "version":
			return true
		}
	}
	return false
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

// configErrorIsFilesOnly reports that ConfigError comes from ValidateFiles alone: the config
// itself is valid, and only a file it points at (the php_ini file) is missing. The teardown
// commands tolerate that case; see configGate.
var configErrorIsFilesOnly bool

// exitOnConfigError ends the process after a refused config. A variable so tests can observe the
// refusal instead of losing the whole test binary to os.Exit.
var exitOnConfigError = os.Exit

// configGate returns the error that must stop cmd before it runs, or nil.
//
// `down` and `clear` are let through a config whose only problem is a missing file, with a
// warning on stderr: they remove containers and never mount anything, and refusing them would
// leave a stack running that the user can no longer stop through orobox — at the very moment the
// file has been moved or deleted. A config that is invalid in itself still stops them, because
// the compose files they run cannot be rendered from it.
func configGate(cmd *cobra.Command) error {
	// help, completion and version print nothing that depends on the config: a user fixing a
	// broken file still needs `orobox help`, and a completion script must not fail to generate.
	if ConfigError == nil || isConfigExempt(cmd) || printsVerbatimOutput(cmd) {
		return nil
	}
	if configErrorIsFilesOnly && isTeardownCommand(cmd) {
		utils.PrintWarningStderr(fmt.Sprintf("%v\nContinuing anyway: '%s' only stops the environment and does not need that file.", ConfigError, cmd.Name()))
		// EnsureDockerCompose would otherwise report the same file again, on stdout.
		docker.SetPhpIniProblemReported()
		return nil
	}
	return ConfigError
}

// isTeardownCommand reports whether cmd is one of the top-level commands that only stop the
// environment. Name() is the primary name, so the `clean` alias of `clear` matches too. The
// parent is checked by shape rather than against rootCmd, which PersistentPreRun is part of and so
// cannot refer to.
func isTeardownCommand(cmd *cobra.Command) bool {
	if cmd.Parent() == nil || cmd.Parent().HasParent() {
		return false
	}
	switch cmd.Name() {
	case downCmd.Name(), cleanCmd.Name():
		return true
	}
	return false
}

// isConfigExempt reports whether a command may run without a valid .orobox.yaml.
// These commands either create the config/source tree or manage the binary itself.
func isConfigExempt(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "init", "self-update", "internal-gen-docker", "create", "extend":
		return true
	}
	// create's subcommands (project, bundle) run before any config exists.
	if cmd.Parent() != nil && cmd.Parent().Name() == "create" {
		return true
	}
	// extend scaffolds files, and one of the things it fixes is a config that points at a Dockerfile
	// that does not exist yet: validating the config first would refuse to create the very file
	// the validation is asking for.
	if cmd.Parent() != nil && cmd.Parent().Name() == "extend" {
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

	// initConfig can run more than once in a process (the run command's help calls it again), so
	// a verdict on an earlier config must not outlive it.
	ConfigError = nil
	configErrorIsFilesOnly = false

	if err := viper.ReadInConfig(); err != nil {
		if isMissingConfigFile(err) {
			return
		}
		// A file that exists but cannot be read or parsed — a YAML syntax error, most often — used
		// to be ignored with the "no config" case, and every command then ran on an empty config.
		configFile := viper.ConfigFileUsed()
		if configFile == "" {
			configFile = ".orobox.yaml"
		}
		ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
		return
	}

	configFile := viper.ConfigFileUsed()
	data, err := os.ReadFile(configFile)
	if err == nil {
		c, err := config.ParseConfig(data)
		if err != nil {
			ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
		} else if err := c.Validate(); err != nil {
			ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
		} else if err := c.ValidateFiles(filepath.Dir(configFile)); err != nil {
			// Validate cannot see the disk, so the files the config points at are
			// checked here, against the directory holding the config.
			ConfigError = fmt.Errorf("invalid config file %s:\n%v", configFile, err)
			configErrorIsFilesOnly = true
		}
	}

	debug := viper.GetBool("debug")

	if ConfigError == nil && debug {
		utils.PrintInfo("Using config file: " + configFile)
	}
}

// isMissingConfigFile reports whether a ReadInConfig error only means there is no config to read:
// no .orobox.yaml in the working directory, or a --config path that does not exist yet (`orobox
// init --config` is how such a file is created). Commands that need a config refuse to run on
// their own when there is none.
func isMissingConfigFile(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	return errors.As(err, &notFound) || errors.Is(err, fs.ErrNotExist)
}
