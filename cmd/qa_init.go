// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

		// The base configurations first: the coding standard's Composer plugin does not write
		// them on every release, and the fix-ups below have nothing to work on without them.
		if base := qatools.BaseConfigScript(); base != "" {
			runQaScript("Writing the base QA configurations...", "Base QA configurations in place.", base)
		}

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

	// 2. Install JS packages in the QA tools namespace directory. The install runs through the
	//    shared shell line rather than as a bare exec because where the packages land is decided
	//    by the manifest that line writes first; see JSInstallCommand.
	//    The command is empty when the enabled tools need no package of their own — stylelint
	//    without eslint — and then there is nothing to run.
	if jsInstall := qatools.JSInstallCommand(plan); jsInstall != "" {
		npmArgs := []string{"exec", "-T", "application", "sh", "-c", jsInstall}
		if err := docker.RunComposeCommandSilently(fmt.Sprintf("Installing %s QA packages...", strings.ToUpper(plan.JSManager)), npmArgs...); err != nil {
			utils.PrintError(fmt.Sprintf("Failed to install %s packages: %v", plan.JSManager, err))
			return err
		}
		utils.PrintSuccess(fmt.Sprintf("%s QA packages installed.", strings.ToUpper(plan.JSManager)))
	}

	writeQaStubs(config.GetHostBundlePath(), conf.Type)

	utils.PrintSuccess("QA tools initialized successfully!")

	offerPreCommitHook(config.GetHostBundlePath())
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

// preCommitHookFile is the hook git runs before it writes a commit.
const preCommitHookFile = "pre-commit"

// offerPreCommitHook asks whether to install the pre-commit hook, and installs it on a yes.
//
// It is the last thing qa-init does, because it is the only part that is worth nothing until the
// tools it calls are installed.
//
// Everything here warns rather than fails, for the same reason writeQaStubs does: the tools are
// installed and usable without a hook, and reporting the whole initialization as failed because a
// git directory could not be read would be misleading.
func offerPreCommitHook(projectDir string) {
	// SkipPrompts rather than isTTY, like every other question Orobox asks: a CI job or the e2e
	// harness inherits a stdin that never produces a newline, and a prompt would hang there.
	if utils.SkipPrompts(stdin) {
		return
	}

	hooksDir, err := gitHooksDir(projectDir)
	if err != nil {
		// A checkout that is not a git repository yet is a normal state right after `orobox
		// create`, and has no hook to install. Nothing to report.
		return
	}

	reader := bufio.NewReader(stdin)
	if !utils.AskYesNo(reader, "Install a git pre-commit hook? It checks the files each commit stages with the enabled QA tools, and runs the tests when the commit touches PHP", true) {
		return
	}

	hookPath := filepath.Join(hooksDir, preCommitHookFile)
	if _, err := os.Stat(hookPath); err == nil {
		if !utils.AskYesNo(reader, fmt.Sprintf("%s already exists. Replace it? The current one is kept as %s.bak", hookPath, preCommitHookFile), false) {
			utils.PrintInfo("Left the existing pre-commit hook in place.")
			return
		}
		if err := os.Rename(hookPath, hookPath+".bak"); err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not back up %s: %v", hookPath, err))
			return
		}
		utils.PrintSuccess("Kept the previous hook as " + hookPath + ".bak.")
	} else if !errors.Is(err, os.ErrNotExist) {
		utils.PrintWarning(fmt.Sprintf("Could not check %s: %v", hookPath, err))
		return
	}

	if err := writePreCommitHook(hooksDir, projectDir, oroboxBinary()); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not install the pre-commit hook: %v", err))
		return
	}
	utils.PrintSuccess("Wrote " + hookPath + " (skip it with OROBOX_SKIP_PRECOMMIT=1 or git commit --no-verify).")
}

// oroboxBinary is the absolute path of the running orobox, which the hook calls instead of the
// bare name; the template says why. A build whose own path cannot be resolved falls back to that
// bare name, which is what the hook itself falls back to anyway.
func oroboxBinary() string {
	binary, err := os.Executable()
	if err != nil {
		return "orobox"
	}
	return binary
}

// writePreCommitHook renders the hook into hooksDir. The binary is a parameter rather than read
// here so the rendered file is decided by one caller and can be asserted whole.
func writePreCommitHook(hooksDir, projectDir, binary string) error {
	rendered, err := scaffold.Render(scaffold.QaHookTemplate, scaffold.QaHookData{
		Binary:     utils.ShellQuote(binary),
		ProjectDir: utils.ShellQuote(projectDir),
	})
	if err != nil {
		return err
	}

	// 0o755 and not the 0o644 the scaffolded files get: git skips a hook it cannot execute, and
	// skips it silently, so a hook written without the bit is a hook that never reports anything.
	return os.WriteFile(filepath.Join(hooksDir, preCommitHookFile), rendered, 0o755)
}

// gitHooksDir resolves where this checkout keeps its hooks, as an absolute path.
//
// core.hooksPath is asked for first because `rev-parse --git-path hooks` does not honour it: it
// answers with the repository's own hooks directory, which for a checkout managed by husky or
// lefthook is not the directory git actually runs. Writing there would install a hook that never
// runs, and never says so.
//
// `rev-parse --git-path` is what covers the rest: a worktree, a submodule and a repository with a
// separate .git file all keep their hooks somewhere other than <root>/.git/hooks.
func gitHooksDir(projectDir string) (string, error) {
	if out, err := exec.Command("git", "-C", projectDir, "config", "--get", "core.hooksPath").Output(); err == nil {
		if configured := strings.TrimSpace(string(out)); configured != "" {
			return absoluteUnder(projectDir, configured), nil
		}
	}

	out, err := exec.Command("git", "-C", projectDir, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a git repository: %w", projectDir, err)
	}

	hooksDir := absoluteUnder(projectDir, strings.TrimSpace(string(out)))
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	return hooksDir, nil
}

// absoluteUnder resolves a path git reported, which is relative to the directory git ran in.
func absoluteUnder(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
