// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
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
	qaToolsDir := config.QaToolsDir

	plan := qatools.NewInstallPlan(conf.OroVersion)

	if !plan.NeedsComposerTools && !plan.NeedsJSTools {
		utils.PrintWarning("No QA tools are enabled in configuration. Nothing to install.")
		return
	}

	// 1. Install PHP packages using bamarni/composer-bin-plugin.
	//    This creates an isolated composer project at vendor-bin/ that shares
	//    the OroCommerce autoloader, so PHPStan can resolve all OroCommerce classes.
	if plan.NeedsComposerTools {
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

		// Remove project's own php-cs-fixer so it doesn't conflict with the QA namespace install.
		removeArgs := []string{"exec", "-w", oroRoot, "-T", "application", "composer", "remove", "--dev", "--no-scripts", "friendsofphp/php-cs-fixer"}
		if err := docker.RunComposeCommandSilently("Removing project php-cs-fixer...", removeArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to remove project php-cs-fixer: %v", err))
			return
		}

		// 1c. Install QA packages in the isolated 'qa' bin namespace.
		//     Using ':*' forces the latest version, bypassing OroCommerce's locked constraints.
		composerArgs := []string{"exec", "-w", oroRoot}
		if !isTTY() {
			composerArgs = append(composerArgs, "-T")
		}
		// Pipe 'yes y' to auto-accept config file generation prompts from the plugin; '|| true'
		// swallows the SIGPIPE exit code 'yes' gets once composer stops reading, which a shell
		// with pipefail would otherwise report as a failed install.
		// -W allows the Symfony pins to downgrade transitively-locked packages on re-runs.
		cmdLine := "(yes y || true) | composer bin qa require --dev -W " + strings.Join(plan.ComposerPackages, " ")
		composerArgs = append(composerArgs, "application", "bash", "-c", cmdLine)

		if err := docker.RunComposeCommand("Installing Composer QA packages...", composerArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
			return
		}
		utils.PrintSuccess("Composer QA packages installed.")

		// The generated phpstan.neon and the missing twig-cs-fixer config both need fixing up
		// for the Oro layout; the deploy pipeline runs the very same scripts.
		if plan.NeedsPhpstan {
			runQaScript("Adapting PHPStan config for Oro layout...", "PHPStan config adapted for Oro layout.", qatools.PhpstanConfigScript())
		}
		if plan.NeedsTwigCS {
			runQaScript("Writing default Twig-CS-Fixer config...", "Twig-CS-Fixer config written.", qatools.TwigConfigScript())
		}
	}

	// 2. Install JS packages in the QA tools namespace directory.
	if plan.NeedsJSTools {
		npmArgs := []string{"exec", "-w", qaToolsDir, "-T", "application", plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag}
		if plan.JSManager == "pnpm" {
			// pnpm refuses to add deps to a workspace root unless told it's intentional
			// (ERR_PNPM_ADDING_TO_ROOT). The QA tools dir is such a root.
			npmArgs = append(npmArgs, "--ignore-workspace-root-check")
		}
		npmArgs = append(npmArgs, plan.JSPackages...)
		if err := docker.RunComposeCommandSilently(fmt.Sprintf("Installing %s QA packages...", strings.ToUpper(plan.JSManager)), npmArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install %s packages: %v", plan.JSManager, err))
			return
		}
		utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(plan.JSManager)))
	}

	utils.PrintSuccess("QA tools initialized successfully!")
}

// runQaScript runs a shell script in the application container, warning rather than failing:
// a missing generated config is not fatal to the rest of the initialization.
func runQaScript(progress, success, script string) {
	args := []string{"exec", "-T", "application", "sh", "-c", script}
	if err := docker.RunComposeCommandSilently(progress, args...); err != nil {
		utils.PrintWarning(fmt.Sprintf("%s failed: %v", progress, err))
		return
	}
	utils.PrintSuccess(success)
}
