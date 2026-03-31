// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	qaPhpstan     bool
	qaRector      bool
	qaPhpCsFixer  bool
	qaTwigCsFixer bool
	qaEslint      bool
	qaStylelint   bool
)

var qaCmd = &cobra.Command{
	Use:   "qa",
	Short: "Run QA tools (PHPStan, Rector, PHP-CS-Fixer, Twig-CS-Fixer, ESLint, Stylelint)",
	Run: func(_ *cobra.Command, _ []string) {
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
	qaCmd.Flags().BoolVar(&qaPhpCsFixer, "php-cs-fixer", false, "Run PHP-CS-Fixer")
	qaCmd.Flags().BoolVar(&qaTwigCsFixer, "twig-cs-fixer", false, "Run Twig-CS-Fixer")
	qaCmd.Flags().BoolVar(&qaEslint, "eslint", false, "Run ESLint")
	qaCmd.Flags().BoolVar(&qaStylelint, "stylelint", false, "Run Stylelint")
}

func runQaCommand() {
	allTools := []struct {
		name    string
		args    []string
		enabled bool
	}{
		{"phpstan", []string{"vendor/bin/phpstan", "analyze"}, qaPhpstan},
		{"rector", []string{"vendor/bin/rector", "process"}, qaRector},
		{"php-cs-fixer", []string{"vendor/bin/php-cs-fixer", "fix"}, qaPhpCsFixer},
		{"twig-cs-fixer", []string{"vendor/bin/twig-cs-fixer", "lint"}, qaTwigCsFixer},
		{"eslint", []string{"npx", "eslint", "--ignore-path", ".eslintignore", "--fix"}, qaEslint},
		{"stylelint", []string{"npx", "stylelint", "--ignore-path", ".stylelintignore", "--fix"}, qaStylelint},
	}

	anyEnabled := false
	for _, tool := range allTools {
		if tool.enabled {
			anyEnabled = true
			break
		}
	}

	isBundle := viper.GetString("type") == config.InstallTypeBundle
	workingDir := config.OroRootDir
	if isBundle {
		workingDir = config.OroRootDir + "/src/" + config.GetBundlePath()
	}
	utils.PrintInfo("Running QA tools in " + workingDir + "...")
	for _, tool := range allTools {
		if anyEnabled && !tool.enabled {
			continue
		}

		utils.PrintInfo(fmt.Sprintf("Running %s...", tool.name))

		args := []string{"exec", "-w", workingDir}
		if !isTTY() {
			args = append(args, "-T")
		}
		args = append(args, "application_test")
		args = append(args, tool.args...)

		err := docker.RunComposeCommand("", args...)
		if err != nil {
			utils.PrintError(fmt.Sprintf("%s reported errors or warnings. Stopping execution.", tool.name))
			os.Exit(1)
		}
		utils.PrintSuccess(fmt.Sprintf("%s completed successfully.", tool.name))
	}

	utils.PrintSuccess("All selected QA tools passed!")
}
