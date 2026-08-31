// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/scaffold"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ciInitCmd = &cobra.Command{
	Use:   "ci-init",
	Short: "Generate the GitLab CI pipeline for this project",
	Long: `Generates two files: .gitlab-ci-orobox.yml, which holds the lint, test and deploy jobs and
is rewritten on every run, and .gitlab-ci.yml, which is created only when absent and does nothing
but include the first one.

The jobs run the commands they are named after — orobox qa, orobox test, orobox deploy <stage> —
with the report paths and deploy stages taken from .orobox.yaml, so a generated pipeline cannot
declare an artifact nothing produces.`,
	Run: func(_ *cobra.Command, _ []string) {
		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			os.Exit(1)
		}

		// The only CI-viable engine is Dagger, and its image exists for the project type alone
		// (algoritmadev/orobox:<version>-project-latest). Any other type falls back to the compose
		// engine, which needs a full composer install and a full oro:install before it can lint —
		// a different pipeline design, not a variation of this one.
		if conf.Type != config.InstallTypeProject {
			utils.PrintError(fmt.Sprintf("ci-init is only available for install type %q; this project is %q, whose CI would have to stand up a full development stack per job",
				config.InstallTypeProject, conf.Type))
			os.Exit(1)
		}

		if err := writeCIFiles(config.GetHostBundlePath(), &conf); err != nil {
			utils.PrintError(err.Error())
			os.Exit(1)
		}

		utils.PrintInfo("Commit " + config.CIIncludeRelPath + " and " + config.CIRootRelPath + ".")
		if !conf.Deploy.Configured() {
			utils.PrintInfo("No deploy stages are configured, so no deploy job was generated. Run 'orobox deploy-init' and then 'orobox ci-init' again.")
		}
	},
}

func init() {
	rootCmd.AddCommand(ciInitCmd)
}

// writeCIFiles generates the pair of pipeline files and, when the project's own pipeline already
// existed without the include, prints the two lines that make a runner read the generated one.
// Writing a pipeline no runner reads would look like success and behave like nothing.
func writeCIFiles(projectDir string, conf *config.OroConfig) error {
	rootPath := filepath.Join(projectDir, config.CIRootRelPath)
	hadRoot := false
	if _, err := os.Stat(rootPath); err == nil {
		hadRoot = true
	}

	results, err := scaffold.WriteAll(projectDir, scaffold.CIArtifacts(), scaffold.NewCIData(Version, conf.Deploy))
	if err != nil {
		return err
	}

	for _, result := range results {
		switch {
		case result.Skipped:
			utils.PrintInfo(result.Artifact.RelPath + " already exists and was left untouched.")
		case result.Artifact.Ownership == scaffold.Rewrite:
			utils.PrintSuccess("Wrote " + result.Artifact.RelPath + " (Orobox rewrites it on every ci-init).")
		default:
			utils.PrintSuccess("Wrote " + result.Artifact.RelPath + " (yours from now on).")
		}
	}

	if !hadRoot {
		return nil
	}

	included, err := includesOroboxPipeline(rootPath)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not check %s for the include: %v", config.CIRootRelPath, err))
		return nil
	}
	if !included {
		utils.PrintWarning("Your " + config.CIRootRelPath + " does not include the generated pipeline. Add:")
		fmt.Println("include:")
		fmt.Println("  - local: " + config.CIIncludeRelPath)
	}

	return nil
}

// includesOroboxPipeline reports whether the project's own pipeline mentions the generated one. A
// missing file is not an error: ci-init generates it in that case.
func includesOroboxPipeline(rootPath string) (bool, error) {
	data, err := os.ReadFile(rootPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), config.CIIncludeRelPath), nil
}
