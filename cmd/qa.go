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
	"github.com/spf13/viper"
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
		if viper.GetString("type") == config.InstallTypeDemo {
			utils.PrintError("The 'qa' command is not available for demo instances.")
			return
		}
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

func runQaCommand() {
	isBundle := viper.GetString("type") == config.InstallTypeBundle
	workingDir := config.OroRootDir
	if isBundle {
		workingDir = config.OroRootDir + "/src/" + config.GetBundlePath()
	}

	jsTarget := "src"
	cssTarget := "src/**/*.{css,scss,less,sass,html}"
	twigTarget := "src"
	if isBundle {
		jsTarget = "Resources/public"
		cssTarget = "Resources/public/**/*.{css,scss,less,sass,html}"
		twigTarget = "."
	}

	type tool struct {
		name    string
		args    []string
		enabled bool
	}

	allTools := []tool{
		{"phpstan", []string{"vendor/bin/phpstan", "analyze"}, qaPhpstan},
		{"rector", []string{"vendor/bin/rector", "process"}, qaRector},
		{"php-cs-fixer", []string{"vendor/bin/php-cs-fixer", "fix"}, qaPhpCSFixer},
		{"twig-cs-fixer", []string{"vendor/bin/twig-cs-fixer", "lint", twigTarget}, qaTwigCSFixer},
	}

	if isBundle {
		allTools = append(allTools,
			tool{"eslint", []string{"npx", "--yes", "eslint", "--config", config.OroRootDir + "/.eslintrc.yml", "--ignore-path", config.OroRootDir + "/.eslintignore", "--fix", "--quiet", jsTarget}, qaEslint},
			tool{"stylelint", []string{"npx", "--yes", "stylelint", "Resources/public/**/*.{scss,less,sass,html}", "--config", config.OroRootDir + "/.stylelintrc.yml", "--ignore-path", config.OroRootDir + "/.stylelintignore", "--fix", "--quiet", "--allow-empty-input"}, qaStylelint},
			tool{"stylelint-css", []string{"npx", "--yes", "stylelint", "Resources/public/**/*.css", "--config", config.OroRootDir + "/.stylelintrc-css.yml", "--ignore-path", config.OroRootDir + "/.stylelintignore-css", "--fix", "--quiet", "--allow-empty-input"}, qaStylelint},
		)
	} else {
		allTools = append(allTools,
			tool{"eslint", []string{"npx", "--yes", "eslint", "--ignore-path", ".eslintignore", "--fix", "--quiet", jsTarget}, qaEslint},
			tool{"stylelint", []string{"npx", "--yes", "stylelint", cssTarget, "--ignore-path", ".stylelintignore", "--fix", "--quiet", "--allow-empty-input"}, qaStylelint},
		)
	}

	anyEnabled := false
	for _, tool := range allTools {
		if tool.enabled {
			anyEnabled = true
			break
		}
	}

	utils.PrintInfo("Running QA tools in " + workingDir + "...")

	var enabledTools []tool
	for _, t := range allTools {
		if !anyEnabled || t.enabled {
			enabledTools = append(enabledTools, t)
		}
	}

	if len(enabledTools) == 0 {
		utils.PrintWarning("No QA tools enabled.")
		return
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
