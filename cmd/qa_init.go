// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var qaInitCmd = &cobra.Command{
	Use:   "qa-init",
	Short: "Initialize QA tools in the project or bundle",
	Run: func(_ *cobra.Command, _ []string) {
		docker.SetIncludeTestFiles(true)
		docker.EnsureDockerCompose()

		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			return
		}

		utils.PrintInfo("Initializing QA tools...")
		runQaInitCommand()
	},
}

func init() {
	rootCmd.AddCommand(qaInitCmd)
}

func runQaInitCommand() {
	workingDir := config.GetBundleRootContainerPath()

	// 1. Configure Composer plugins
	utils.PrintInfo("Configuring Composer plugins (phpstan/extension-installer, algoritma/php-coding-standards)...")
	for _, plugin := range []string{"phpstan/extension-installer", "algoritma/php-coding-standards"} {
		configArgs := []string{"exec", "-w", workingDir}
		if !isTTY() {
			configArgs = append(configArgs, "-T")
		}
		configArgs = append(configArgs, "application", "composer", "config", "--no-plugins", "allow-plugins."+plugin, "true")
		if err := docker.RunComposeCommand("", configArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to configure plugin %s: %v", plugin, err))
			return
		}
	}

	// 2. Install Composer packages
	utils.PrintInfo("Installing Composer QA packages (algoritma/php-coding-standards, vincentlanglet/twig-cs-fixer)...")
	composerArgs := []string{"exec", "-w", workingDir}
	if !isTTY() {
		composerArgs = append(composerArgs, "-T")
	}
	// Use bash -c to pipe 'yes' into composer to automatically accept file creation from the plugin
	cmdLine := "yes y | composer require --dev algoritma/php-coding-standards vincentlanglet/twig-cs-fixer"
	composerArgs = append(composerArgs, "application", "bash", "-c", cmdLine)

	if err := docker.RunComposeCommand("", composerArgs...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
		return
	}

	// 3. Install NPM packages
	utils.PrintInfo("Installing NPM QA packages (eslint@^8.57.0, eslint-plugin-no-jquery, stylelint@^15.11.0, @oroinc/oro-stylelint-config, eslint-plugin-import)...")
	npmArgs := []string{"exec", "-w", workingDir}
	if !isTTY() {
		npmArgs = append(npmArgs, "-T")
	}
	npmArgs = append(npmArgs, "application", "npm", "install", "--save-dev", "eslint@^8.57.0", "eslint-plugin-no-jquery", "stylelint@^15.11.0", "@oroinc/oro-stylelint-config", "eslint-plugin-import")

	if err := docker.RunComposeCommand("", npmArgs...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to install NPM packages: %v", err))
		return
	}

	utils.PrintSuccess("QA tools initialized successfully!")
}
