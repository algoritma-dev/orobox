// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"encoding/base64"
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
	qaToolsDir := config.QaToolsDir

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
	//    This creates an isolated composer project at vendor-bin/ that shares
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
			composerPackages = append(composerPackages, "phpstan/phpstan-symfony", "phpstan/phpstan-phpunit", "phpstan/phpstan-doctrine", "algoritma/php-coding-standards:*")
			// Pin the tools' Symfony components to Oro's line so PHPStan can co-load
			// Oro's classes with them without fatal signature mismatches.
			composerPackages = append(composerPackages, config.GetQaSymfonyConstraints(conf.OroVersion)...)
		}
		if needsTwigCS {
			composerPackages = append(composerPackages, "vincentlanglet/twig-cs-fixer:*")
		}

		// Remove project's own php-cs-fixer so it doesn't conflict with the QA namespace install.
		removeArgs := []string{"exec", "-w", oroRoot, "-T", "application", "composer", "remove", "--dev", "--no-scripts", "friendsofphp/php-cs-fixer"}
		if err := docker.RunComposeCommandSilently("Removing project php-cs-fixer...", removeArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to remove project php-cs-fixer: %v", err))
			return
		}

		composerArgs := []string{"exec", "-w", oroRoot}
		if !isTTY() {
			composerArgs = append(composerArgs, "-T")
		}
		// Pipe 'yes y' to auto-accept config file generation prompts from the plugin.
		// -W allows the Symfony pins to downgrade transitively-locked packages on re-runs.
		cmdLine := "yes y | composer bin qa require --dev -W " + strings.Join(composerPackages, " ")
		composerArgs = append(composerArgs, "application", "bash", "-c", cmdLine)

		if err := docker.RunComposeCommand("Installing Composer QA packages...", composerArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install Composer packages: %v", err))
			return
		}
		utils.PrintSuccess("Composer QA packages installed.")

		// The algoritma/php-coding-standards plugin generates phpstan.neon assuming it
		// lives at the application root, writing paths relative to it (and hardcoding
		// Symfony's App_Kernel container filename). Because bamarni places the config in
		// the isolated QaToolsDir, PHPStan resolves those paths against QaToolsDir and
		// fails ("Scanned file ... does not exist"). Rewrite them to absolute OroRoot
		// paths and replace the stub bootstrap loaders with Oro-aware ones.
		if config.IsQaToolEnabled("phpstan") {
			fixPhpstanConfigForOro()
		}
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

		npmArgs := []string{"exec", "-w", qaToolsDir, "-T", "application", jsManager, jsInstallCmd, jsSaveDevFlag}
		if jsManager == "pnpm" {
			// pnpm refuses to add deps to a workspace root unless told it's intentional
			// (ERR_PNPM_ADDING_TO_ROOT). The QA tools dir is such a root.
			npmArgs = append(npmArgs, "--ignore-workspace-root-check")
		}
		npmArgs = append(npmArgs, jsPackages...)
		if err := docker.RunComposeCommandSilently(fmt.Sprintf("Installing %s QA packages...", strings.ToUpper(jsManager)), npmArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install %s packages: %v", jsManager, err))
			return
		}
		utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(jsManager)))
	}

	utils.PrintSuccess("QA tools initialized successfully!")
}

// oroKernelClass is the OroCommerce application kernel. The Symfony container is
// dumped to <KernelClass><Env>DebugContainer.xml, so this also drives the
// containerXmlPath filename PHPStan expects.
const oroKernelClass = "AppKernel"

// consoleApplicationLoaderPHP boots the real Oro kernel so phpstan-symfony can
// enumerate console commands. It uses OroRoot's autoloader (not the isolated QA
// one, which lacks the application classes). It boots the 'dev' env to match the
// containerXmlPath below: dev is warmed and its config is complete, whereas the
// 'test' env qa.go exports references files (e.g. security_test.yml) that need a
// full test setup PHPStan does not require.
const consoleApplicationLoaderPHP = `<?php

declare(strict_types=1);

use Symfony\Bundle\FrameworkBundle\Console\Application;

require '` + config.OroRootDir + `/vendor/autoload.php';
require_once '` + config.OroRootDir + `/src/` + oroKernelClass + `.php';

$kernel = new ` + oroKernelClass + `('dev', true);

return new Application($kernel);
`

// objectManagerLoaderPHP returns Oro's Doctrine ObjectManager for phpstan-doctrine,
// replacing the throwing stub the plugin generates. Boots 'dev' for the same
// reason as the console loader above.
const objectManagerLoaderPHP = `<?php

declare(strict_types=1);

require '` + config.OroRootDir + `/vendor/autoload.php';
require_once '` + config.OroRootDir + `/src/` + oroKernelClass + `.php';

$kernel = new ` + oroKernelClass + `('dev', true);
$kernel->boot();

return $kernel->getContainer()->get('doctrine')->getManager();
`

// fixPhpstanConfigForOro rewrites the generated vendor-bin/qa/phpstan.neon so its
// paths resolve from OroRoot instead of the isolated QaToolsDir, and installs
// Oro-aware bootstrap loaders. See the caller for why this is necessary.
func fixPhpstanConfigForOro() {
	oro := config.OroRootDir
	qa := config.QaToolsDir
	neon := qa + "/phpstan.neon"

	consoleAbs := qa + "/tests/console-application.php"
	objAbs := qa + "/tests/object-manager.php"
	xmlAbs := fmt.Sprintf("%s/var/cache/dev/%sDevDebugContainer.xml", oro, oroKernelClass)
	scanDirAbs := oro + "/var/cache/dev/Symfony/Config"
	scanFileAbs := oro + "/vendor/symfony/dependency-injection/Loader/Configurator/ContainerConfigurator.php"

	b64Console := base64.StdEncoding.EncodeToString([]byte(consoleApplicationLoaderPHP))
	b64Obj := base64.StdEncoding.EncodeToString([]byte(objectManagerLoaderPHP))

	// Anchoring each replacement to its key/list-marker keeps this idempotent: once a
	// value is absolute it no longer matches the relative pattern on a re-run.
	script := fmt.Sprintf(`set -e
[ -f %[1]s ] || exit 0
mkdir -p %[2]s/tests
sed -i \
 -e 's#consoleApplicationLoader: tests/console-application.php#consoleApplicationLoader: %[3]s#' \
 -e 's#objectManagerLoader: tests/object-manager.php#objectManagerLoader: %[4]s#' \
 -e 's#containerXmlPath: var/cache/dev/App_KernelDevDebugContainer.xml#containerXmlPath: %[5]s#' \
 -e 's#- var/cache/dev/Symfony/Config#- %[6]s#' \
 -e 's#- vendor/symfony/dependency-injection/Loader/Configurator/ContainerConfigurator.php#- %[7]s#' \
 %[1]s
printf '%%s' '%[8]s' | base64 -d > %[3]s
printf '%%s' '%[9]s' | base64 -d > %[4]s`,
		neon, qa, consoleAbs, objAbs, xmlAbs, scanDirAbs, scanFileAbs, b64Console, b64Obj)

	args := []string{"exec", "-T", "application", "sh", "-c", script}
	if err := docker.RunComposeCommandSilently("Adapting PHPStan config for Oro layout...", args...); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not adapt PHPStan config for Oro: %v", err))
		return
	}
	utils.PrintSuccess("PHPStan config adapted for Oro layout.")
}
