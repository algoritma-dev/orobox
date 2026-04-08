// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"strings"

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
		runQaInitCommand(conf)
	},
}

func init() {
	rootCmd.AddCommand(qaInitCmd)
}

func runQaInitCommand(conf config.OroConfig) {
	workingDir := config.GetBundleRootContainerPath()

	// 1. Configure Composer plugins
	for _, plugin := range []string{"phpstan/extension-installer", "algoritma/php-coding-standards"} {
		configArgs := []string{"exec", "-w", workingDir}
		if !isTTY() {
			configArgs = append(configArgs, "-T")
		}
		configArgs = append(configArgs, "application", "composer", "config", "--no-plugins", "allow-plugins."+plugin, "true")
		if err := docker.RunComposeCommandSilently("Configuring Composer plugin "+plugin, configArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to configure plugin %s: %v", plugin, err))
			return
		}
	}
	utils.PrintSuccess("Composer plugins configured.")

	// 2. Install Composer packages
	composerArgs := []string{"exec", "-w", workingDir}
	if !isTTY() {
		composerArgs = append(composerArgs, "-T")
	}
	// Use bash -c to pipe 'yes' into composer to automatically accept file creation from the plugin
	cmdLine := "yes y | composer require --dev algoritma/php-coding-standards vincentlanglet/twig-cs-fixer"
	composerArgs = append(composerArgs, "application", "bash", "-c", cmdLine)

	if err := docker.RunComposeCommandSilently("Installing Composer QA packages...", composerArgs...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
		return
	}
	utils.PrintSuccess("Composer QA packages installed.")

	// 3. Install NPM/PNPM packages
	versions := config.GetVersionsForOro(conf.OroVersion)
	jsManager := "npm"
	jsInstallCmd := "install"
	jsSaveDevFlag := "--save-dev"
	if versions.PNPM != "" {
		jsManager = "pnpm"
		jsInstallCmd = "add"
		jsSaveDevFlag = "-D"
	}

	npmArgs := []string{"exec", "-w", workingDir}
	if !isTTY() {
		npmArgs = append(npmArgs, "-T")
	}
	npmArgs = append(npmArgs, "application", jsManager, jsInstallCmd, jsSaveDevFlag, "eslint@^8.57.0", "eslint-plugin-no-jquery", "stylelint@^15.11.0", "@oroinc/oro-stylelint-config", "eslint-plugin-import")

	if err := docker.RunComposeCommandSilently(fmt.Sprintf("Installing %s QA packages...", strings.ToUpper(jsManager)), npmArgs...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to install %s packages: %v", jsManager, err))
		return
	}
	utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(jsManager)))

	utils.PrintSuccess("QA tools initialized successfully!")
}
