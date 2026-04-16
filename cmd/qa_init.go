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
	oroRoot := config.OroRootDir
	qaToolsDir := config.QaToolsDir // /var/www/oro/vendor/bin-dir/qa

	needsPhpCodingStandards := config.IsQaToolEnabled("phpstan") || config.IsQaToolEnabled("rector") || config.IsQaToolEnabled("php-cs-fixer")
	needsTwigCS := config.IsQaToolEnabled("twig-cs-fixer")
	needsEslint := config.IsQaToolEnabled("eslint")
	needsStylelint := config.IsQaToolEnabled("stylelint")
	needsComposerTools := needsPhpCodingStandards || needsTwigCS
	needsJsTools := needsEslint || needsStylelint

	if !needsComposerTools && !needsJsTools {
		utils.PrintWarning("No QA tools are enabled in configuration. Nothing to install.")
		return
	}

	// 1. Install PHP packages using bamarni/composer-bin-plugin.
	//    This creates an isolated composer project at vendor-bin/qa/ that shares
	//    the OroCommerce autoloader, so PHPStan can resolve all OroCommerce classes.
	if needsComposerTools {
		// 1a. Ensure the bin namespace directory and a minimal composer.json exist,
		//     then use 'composer -d' to set allow-plugins — this works even if the file
		//     was previously created by bamarni without the required plugin authorizations.
		initCmd := fmt.Sprintf(
			`mkdir -p %s && [ -f %s/composer.json ] || printf '{"name":"orobox/qa-tools"}' > %s/composer.json`,
			qaToolsDir, qaToolsDir, qaToolsDir,
		)
		initArgs := []string{"exec", "-T", "application", "sh", "-c", initCmd}
		if err := docker.RunComposeCommandSilently("Preparing QA tools namespace...", initArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to prepare QA tools namespace: %v", err))
			return
		}

		for _, plugin := range []string{"phpstan/extension-installer", "algoritma/php-coding-standards"} {
			configArgs := []string{"exec", "-T", "application", "composer", "-d", qaToolsDir, "config", "--no-plugins", "allow-plugins." + plugin, "true"}
			if err := docker.RunComposeCommandSilently("Allowing plugin "+plugin+" in QA namespace...", configArgs...); err != nil {
				utils.PrintError(fmt.Sprintf("Failed to allow plugin %s: %v", plugin, err))
				return
			}
		}
		utils.PrintSuccess("QA namespace configured.")

		// 1b. Allow and install bamarni/composer-bin-plugin in OroRoot.
		for _, step := range []struct {
			msg  string
			args []string
		}{
			{
				"Configuring bamarni/composer-bin-plugin...",
				[]string{"exec", "-w", oroRoot, "-T", "application", "composer", "config", "--no-plugins", "allow-plugins.bamarni/composer-bin-plugin", "true"},
			},
			{
				"Installing bamarni/composer-bin-plugin...",
				[]string{"exec", "-w", oroRoot, "-T", "application", "composer", "require", "--dev", "--no-scripts", "bamarni/composer-bin-plugin"},
			},
		} {
			if err := docker.RunComposeCommandSilently(step.msg, step.args...); err != nil {
				utils.PrintError(fmt.Sprintf("%s failed: %v", step.msg, err))
				return
			}
		}
		utils.PrintSuccess("bamarni/composer-bin-plugin installed.")

		// 1c. Install QA packages in the isolated 'qa' bin namespace.
		//     Using ':*' forces the latest version, bypassing OroCommerce's locked constraints.
		var composerPackages []string
		if needsPhpCodingStandards {
			composerPackages = append(composerPackages, "algoritma/php-coding-standards:*")
		}
		if needsTwigCS {
			composerPackages = append(composerPackages, "vincentlanglet/twig-cs-fixer:*")
		}

		composerArgs := []string{"exec", "-w", oroRoot}
		if !isTTY() {
			composerArgs = append(composerArgs, "-T")
		}
		// Pipe 'yes y' to auto-accept config file generation prompts from the plugin.
		cmdLine := "yes y | composer bin qa require --dev " + strings.Join(composerPackages, " ")
		composerArgs = append(composerArgs, "application", "bash", "-c", cmdLine)

		if err := docker.RunComposeCommand("Installing Composer QA packages...", composerArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
			return
		}
		utils.PrintSuccess("Composer QA packages installed.")
	}

	// 2. Install JS packages in the QA tools namespace directory.
	if needsJsTools {
		versions := config.GetVersionsForOro(conf.OroVersion)
		jsManager := "npm"
		jsInstallCmd := "install"
		jsSaveDevFlag := "--save-dev"
		if versions.PNPM != "" {
			jsManager = "pnpm"
			jsInstallCmd = "add"
			jsSaveDevFlag = "-D"
		}

		var jsPackages []string
		if needsEslint {
			jsPackages = append(jsPackages, "eslint@^8.57.0", "eslint-plugin-no-jquery", "eslint-plugin-import")
		}
		if needsStylelint {
			jsPackages = append(jsPackages, "stylelint@^15.11.0", "@oroinc/oro-stylelint-config")
		}

		npmArgs := []string{"exec", "-w", qaToolsDir}
		if !isTTY() {
			npmArgs = append(npmArgs, "-T")
		}
		npmArgs = append(npmArgs, "application", jsManager, jsInstallCmd, jsSaveDevFlag)
		npmArgs = append(npmArgs, jsPackages...)
		if err := docker.RunComposeCommandSilently(fmt.Sprintf("Installing %s QA packages...", strings.ToUpper(jsManager)), npmArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install %s packages: %v", jsManager, err))
			return
		}
		utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(jsManager)))
	}

	utils.PrintSuccess("QA tools initialized successfully!")
}
