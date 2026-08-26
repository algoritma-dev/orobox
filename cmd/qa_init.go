// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/scaffold"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var qaInitCmd = &cobra.Command{
	Use:   "qa-init",
	Short: "Initialize QA tools in the project or bundle",
	// A composer or npm install that fails is a runtime problem, not a usage problem.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		docker.SetIncludeTestFiles(true)
		docker.EnsureDockerCompose()

		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			return err
		}

		utils.PrintInfo("Initializing QA tools...")
		return runQaInitCommand(conf)
	},
}

func init() {
	rootCmd.AddCommand(qaInitCmd)
}

// runQaInitCommand returns the error rather than only printing it: the tools it installs are
// what `orobox qa` runs, so an install that stopped halfway and exited 0 left every later
// command reporting a missing binary instead of the failure that caused it.
func runQaInitCommand(conf config.OroConfig) error {
	oroRoot := config.OroRootDir
	qaToolsDir := config.QaToolsDir

	plan := qatools.NewInstallPlan(conf.OroVersion)

	if !plan.NeedsComposerTools && !plan.NeedsJSTools {
		utils.PrintWarning("No QA tools are enabled in configuration. Nothing to install.")
		return nil
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
			return err
		}

		for _, plugin := range []string{"phpstan/extension-installer", "algoritma/php-coding-standards"} {
			configArgs := []string{"exec", "-T", "application", "composer", "-d", qaToolsDir, "config", "--no-plugins", "allow-plugins." + plugin, "true"}
			if err := docker.RunComposeCommandSilently("Allowing plugin "+plugin+" in QA namespace...", configArgs...); err != nil {
				utils.PrintError(fmt.Sprintf("Failed to allow plugin %s: %v", plugin, err))
				return err
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
				return err
			}
		}
		utils.PrintSuccess("bamarni/composer-bin-plugin installed.")

		// Remove project's own php-cs-fixer so it doesn't conflict with the QA namespace install.
		removeArgs := []string{"exec", "-w", oroRoot, "-T", "application", "composer", "remove", "--dev", "--no-scripts", "friendsofphp/php-cs-fixer"}
		if err := docker.RunComposeCommandSilently("Removing project php-cs-fixer...", removeArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to remove project php-cs-fixer: %v", err))
			return err
		}

		// 1bis. Hand the packages the application already ships over to its tree: one copy of a
		//       shared package is the difference between PHPStan running and PHPStan fatally
		//       redeclaring a Symfony interface. It runs after the application's own tree is
		//       final — the bamarni install and the php-cs-fixer removal both touch it — because
		//       the patch records the versions actually installed there.
		runQaScript("Sharing the application's vendor tree with the QA tools...", "QA tools pointed at the shared vendor tree.", qatools.SharedVendorScript())

		// 1c. Populate the isolated 'qa' bin namespace: install a committed manifest as-is,
		//     otherwise require the packages with ':*', which forces the latest version and
		//     bypasses OroCommerce's locked constraints.
		composerArgs := []string{"exec", "-w", oroRoot}
		if !isTTY() {
			composerArgs = append(composerArgs, "-T")
		}
		composerArgs = append(composerArgs, "application", "bash", "-c", qatools.ComposerInstallCommand(plan.ComposerPackages))

		if err := docker.RunComposeCommand("Installing Composer QA packages...", composerArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
			return err
		}
		utils.PrintSuccess("Composer QA packages installed.")

		// The generated phpstan.neon and the missing twig-cs-fixer config both need fixing up
		// for the Oro layout; the deploy pipeline runs the very same scripts.
		if plan.NeedsPhpstan {
			// The generated config carries the cache paths of one environment, so it is written
			// for the same one `orobox qa` will run in.
			runQaScript("Adapting PHPStan config for Oro layout...", "PHPStan config adapted for Oro layout.", qatools.PhpstanConfigScript(resolveQaEnv()))
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
			return err
		}
		utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(plan.JSManager)))
	}

	writeQaStubs(config.GetHostBundlePath(), conf.Type)

	utils.PrintSuccess("QA tools initialized successfully!")
	return nil
}

// writeQaStubs writes the QA configuration stubs into the project's own checkout, so the files the
// QA run already layers on top of the shared standard are visible and correctly shaped instead of
// having to be guessed.
//
// It warns rather than fails, like runQaScript below: the tools are installed and usable without
// the stubs, and an install that reported failure because one stub could not be written would be
// misleading.
func writeQaStubs(projectDir, typeName string) {
	stubs := scaffold.QaStubs(typeName)
	if len(stubs) == 0 {
		return
	}

	results, err := scaffold.WriteAll(projectDir, stubs, scaffold.QaStubDataFor(typeName))
	for _, result := range results {
		if result.Written {
			utils.PrintSuccess("Wrote " + result.Artifact.RelPath + " (yours from now on).")
		}
	}
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not write every QA configuration stub: %v", err))
	}
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
