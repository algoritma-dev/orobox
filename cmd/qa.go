// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var (
	qaPhpstan     bool
	qaRector      bool
	qaPhpCSFixer  bool
	qaTwigCSFixer bool
	qaEslint      bool
	qaStylelint   bool
)

var qaCmd = &cobra.Command{
	Use:   "qa",
	Short: "Run QA tools (PHPStan, Rector, PHP-CS-Fixer, Twig-CS-Fixer, ESLint, Stylelint)",
	Run: func(_ *cobra.Command, _ []string) {
		docker.SetIncludeTestFiles(true)
		docker.EnsureDockerCompose()
		utils.PrintInfo("Running QA tools...")
		runQaCommand()
	},
}

func init() {
	rootCmd.AddCommand(qaCmd)

	qaCmd.Flags().BoolVar(&qaPhpstan, "phpstan", false, "Run PHPStan")
	qaCmd.Flags().BoolVar(&qaRector, "rector", false, "Run Rector")
	qaCmd.Flags().BoolVar(&qaPhpCSFixer, "php-cs-fixer", false, "Run PHP-CS-Fixer")
	qaCmd.Flags().BoolVar(&qaTwigCSFixer, "twig-cs-fixer", false, "Run Twig-CS-Fixer")
	qaCmd.Flags().BoolVar(&qaEslint, "eslint", false, "Run ESLint")
	qaCmd.Flags().BoolVar(&qaStylelint, "stylelint", false, "Run Stylelint")
}

// checkMissingToolBinaries returns the names of tools whose binaries are not present in the container.
func checkMissingToolBinaries(workingDir string, tools []qatools.Tool) []string {
	seen := map[string]bool{}
	var checks []string

	for _, t := range tools {
		binPath, ok := qatools.BinaryPaths[t.Name]
		if !ok || seen[binPath] {
			continue
		}
		seen[binPath] = true
		checks = append(checks, fmt.Sprintf("test -f %s || printf 'MISSING:%s\\n'", binPath, t.Name))
	}

	if len(checks) == 0 {
		return nil
	}

	args := []string{"exec", "-w", workingDir, "-T", "application", "sh", "-c", strings.Join(checks, "; ")}
	output, _ := docker.RunComposeCommandWithOutput(args...)

	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MISSING:") {
			missing = append(missing, strings.TrimPrefix(line, "MISSING:"))
		}
	}
	return missing
}

// qaFlagFor maps a tool name to the CLI flag that selects it explicitly.
func qaFlagFor(name string) bool {
	switch name {
	case "phpstan":
		return qaPhpstan
	case "rector":
		return qaRector
	case "php-cs-fixer":
		return qaPhpCSFixer
	case "twig-cs-fixer":
		return qaTwigCSFixer
	case "eslint":
		return qaEslint
	case "stylelint", "stylelint-css":
		return qaStylelint
	default:
		return false
	}
}

func runQaCommand() {
	workingDir := config.GetSourceRootContainerPath()

	// Locally the tools may fix what they find; the deploy pipeline runs the same list in
	// check-only mode.
	allTools := qatools.Tools(workingDir, config.GetQaAnalyzePath(), qatools.ModeFix)

	anyEnabled := false
	for _, t := range allTools {
		if qaFlagFor(t.Name) {
			anyEnabled = true
			break
		}
	}

	utils.PrintInfo("Running QA tools in " + workingDir + "...")

	var enabledTools []qatools.Tool
	for _, t := range allTools {
		if anyEnabled {
			if qaFlagFor(t.Name) {
				enabledTools = append(enabledTools, t)
			}
		} else if config.IsQaToolEnabled(t.Name) {
			enabledTools = append(enabledTools, t)
		}
	}

	if len(enabledTools) == 0 {
		utils.PrintWarning("No QA tools enabled.")
		return
	}

	if missing := checkMissingToolBinaries(workingDir, enabledTools); len(missing) > 0 {
		utils.PrintWarning(fmt.Sprintf("The following QA tools are enabled but not installed: %s", strings.Join(missing, ", ")))
		utils.PrintWarning("Run 'orobox qa-init' to install the missing tools.")
		os.Exit(1)
	}

	args := []string{"exec"}
	args = append(args, "-w", workingDir)
	if !isTTY() {
		args = append(args, "-T")
	}

	// Always set ORO_ENV to test for QA tools
	args = append(args, "-e", "ORO_ENV=test")
	args = append(args, "application", "sh", "-c", qatools.Script(enabledTools))

	err := docker.RunComposeCommand("", args...)
	if err != nil {
		utils.PrintError("QA tools reported errors or warnings.")
		os.Exit(1)
	}

	utils.PrintSuccess("All selected QA tools passed!")
}
