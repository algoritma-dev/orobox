// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
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

type qaTool struct {
	name    string
	args    []string
	enabled bool
}

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

// qaToolBinaryPaths maps tool names to their expected binary paths relative to the bundle working dir.
var qaToolBinaryPaths = map[string]string{
	"phpstan":       "vendor/bin/phpstan",
	"rector":        "vendor/bin/rector",
	"php-cs-fixer":  "vendor/bin/php-cs-fixer",
	"twig-cs-fixer": "vendor/bin/twig-cs-fixer",
	"eslint":        "node_modules/.bin/eslint",
	"stylelint":     "node_modules/.bin/stylelint",
	"stylelint-css": "node_modules/.bin/stylelint",
}

// checkMissingToolBinaries returns the names of tools whose binaries are not present in the container.
func checkMissingToolBinaries(workingDir string, tools []qaTool) []string {
	seen := map[string]bool{}
	var checks []string

	for _, t := range tools {
		binPath, ok := qaToolBinaryPaths[t.name]
		if !ok || seen[binPath] {
			continue
		}
		seen[binPath] = true
		checks = append(checks, fmt.Sprintf("test -f %s || printf 'MISSING:%s\\n'", binPath, t.name))
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

func runQaCommand() {
	workingDir := config.GetBundleRootContainerPath()

	jsTarget := "src/Resources/public"
	twigTarget := "src/Resources/views"

	allTools := []qaTool{
		{"phpstan", []string{"vendor/bin/phpstan", "analyze"}, qaPhpstan},
		{"rector", []string{"vendor/bin/rector", "process"}, qaRector},
		{"php-cs-fixer", []string{"vendor/bin/php-cs-fixer", "fix"}, qaPhpCSFixer},
		{"twig-cs-fixer", []string{"vendor/bin/twig-cs-fixer", "lint", twigTarget, "--fix"}, qaTwigCSFixer},
		{"eslint", []string{"npx", "--yes", "eslint", "--resolve-plugins-relative-to", workingDir, "--config", config.OroRootDir + "/.eslintrc.yml", "--ignore-path", config.OroRootDir + "/.eslintignore", "--fix", "--quiet", jsTarget}, qaEslint},
		{"stylelint", []string{"npx", "--yes", "stylelint", "Resources/public/**/*.{scss,less,sass,html}", "--config", config.OroRootDir + "/.stylelintrc.yml", "--ignore-path", config.OroRootDir + "/.stylelintignore", "--fix", "--quiet", "--allow-empty-input"}, qaStylelint},
		{"stylelint-css", []string{"npx", "--yes", "stylelint", "Resources/public/**/*.css", "--config", config.OroRootDir + "/.stylelintrc-css.yml", "--ignore-path", config.OroRootDir + "/.stylelintignore-css", "--fix", "--quiet", "--allow-empty-input"}, qaStylelint},
	}

	anyEnabled := false
	for _, t := range allTools {
		if t.enabled {
			anyEnabled = true
			break
		}
	}

	utils.PrintInfo("Running QA tools in " + workingDir + "...")

	var enabledTools []qaTool
	for _, t := range allTools {
		if anyEnabled {
			if t.enabled {
				enabledTools = append(enabledTools, t)
			}
		} else if config.IsQaToolEnabled(t.name) {
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

	var compositeCmd strings.Builder
	for i, t := range enabledTools {
		if i > 0 {
			compositeCmd.WriteString(" && ")
		}
		// Wrap each command with an echo for better visibility
		compositeCmd.WriteString(fmt.Sprintf("echo '--- Running %s ---' && ", t.name))
		compositeCmd.WriteString(strings.Join(t.args, " "))
	}

	args := []string{"exec"}
	args = append(args, "-w", workingDir)
	if !isTTY() {
		args = append(args, "-T")
	}

	// Always set ORO_ENV to test for QA tools
	args = append(args, "-e", "ORO_ENV=test")
	args = append(args, "application", "sh", "-c", compositeCmd.String())

	err := docker.RunComposeCommand("", args...)
	if err != nil {
		utils.PrintError("QA tools reported errors or warnings.")
		os.Exit(1)
	}

	utils.PrintSuccess("All selected QA tools passed!")
}
