// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/pipeline"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
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

	qaEngine         string
	qaReport         string
	qaReportPath     string
	qaCacheScope     string
	qaBaseCacheScope string
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

	qaCmd.Flags().StringVar(&qaEngine, "engine", "", "Where the checks run: compose or dagger (default: dagger in CI, compose otherwise)")
	qaCmd.Flags().StringVar(&qaReport, "report", "", "Emit a machine-readable report: gitlab")
	qaCmd.Flags().StringVar(&qaReportPath, "report-path", "", "Where to write the report (default: "+reportsRelDir+"/code-quality.json)")
	qaCmd.Flags().StringVar(&qaCacheScope, "cache-scope", "", "Name the cache volume family, dagger engine only (default: the current branch)")
	qaCmd.Flags().StringVar(&qaBaseCacheScope, "base-cache-scope", "", "Seed a missing test database dump from this cache scope, dagger engine only")
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
	engine, err := resolveEngine(qaEngine)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}
	format, err := resolveReport(qaReport)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	if engine == engineDagger {
		runQaOnDagger(format)
		return
	}
	runQaOnCompose(format)
}

func runQaOnCompose(format qatools.Report) {
	workingDir := config.GetSourceRootContainerPath()

	reportPath := ""
	containerReportDir := ""
	if format != qatools.ReportNone {
		var err error
		reportPath, err = resolveReportPath(qaReportPath, reportsRelDir+"/code-quality.json")
		if err != nil {
			utils.PrintError(err.Error())
			os.Exit(1)
		}
		// The source root is bind-mounted, so a file the container writes below it is already on
		// the host: no copy step, and the same path works for every install type.
		containerReportDir = workingDir + "/" + rawReportsRelDir + "/qa"
	}

	// Locally the tools may fix what they find; the deploy pipeline runs the same list in
	// check-only mode.
	allTools := qatools.Tools(qatools.ToolsOptions{
		SourceRoot:  workingDir,
		AnalyzePath: config.GetQaAnalyzePath(),
		Mode:        qatools.ModeFix,
		Report:      format,
		ReportDir:   containerReportDir,
	})

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

	script := qatools.Script(enabledTools)
	if format != qatools.ReportNone {
		script = qatools.ReportScript(enabledTools, containerReportDir)
	}
	args = append(args, "application", "sh", "-c", script)

	err := docker.RunComposeCommand("", args...)

	if format != qatools.ReportNone {
		// The script exits 0 whatever the tools concluded, so the outcome comes from the status
		// file and the reports are merged either way.
		finishQaReport(rawReportDir("qa"), reportPath, engineCompose, err)
		return
	}

	if err != nil {
		utils.PrintError("QA tools reported errors or warnings.")
		os.Exit(1)
	}

	utils.PrintSuccess("All selected QA tools passed!")
}

// runQaOnDagger runs the pipeline's own QA step: the same tools, the same check-only mode and the
// same cache volumes a deploy of this branch would use.
func runQaOnDagger(format qatools.Report) {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		utils.PrintError(fmt.Sprintf("error reading config: %v", err))
		os.Exit(1)
	}

	projectDir := config.GetHostBundlePath()
	reportPath := ""
	if format != qatools.ReportNone {
		var err error
		reportPath, err = resolveReportPath(qaReportPath, reportsRelDir+"/code-quality.json")
		if err != nil {
			utils.PrintError(err.Error())
			os.Exit(1)
		}
	}

	plan := pipeline.NewChecks(&conf, pipeline.ChecksOptions{
		ProjectDir:     projectDir,
		CacheScope:     resolveCacheScope(qaCacheScope),
		BaseCacheScope: qaBaseCacheScope,
		RunQA:          true,
		Report:         format,
	})

	utils.PrintInfo("Running the QA tools in the pipeline engine. The first run has no caches and takes a while.")

	result, runErr := pipeline.Run(context.Background(), plan, pipeline.Options{
		ProjectDir:    projectDir,
		Debug:         viper.GetBool("debug"),
		ReportHostDir: filepath.Join(projectDir, rawReportsRelDir),
	})

	if format != qatools.ReportNone {
		finishQaReport(result.QAReportDir, reportPath, engineDagger, runErr)
		return
	}

	if runErr != nil {
		utils.PrintError(runErr.Error())
		os.Exit(1)
	}
	utils.PrintSuccess("All selected QA tools passed!")
}

// finishQaReport merges the per-tool files into the single document GitLab reads and turns the
// step's recorded status back into an exit code.
//
// runErr is the engine's own error, which in report mode can only be a real failure — a container
// that could not start, an image that could not be pulled — because the tools' own exit codes are
// swallowed by the report script on purpose.
func finishQaReport(rawDir, reportPath, engine string, runErr error) {
	if runErr != nil {
		utils.PrintError(runErr.Error())
		os.Exit(1)
	}
	if rawDir == "" {
		utils.PrintError("the QA step produced no report directory")
		os.Exit(1)
	}

	entries, err := os.ReadDir(rawDir)
	if err != nil {
		utils.PrintError(fmt.Sprintf("could not read the raw reports in %s: %v", rawDir, err))
		os.Exit(1)
	}

	var reports []report.ToolReport
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rawDir, entry.Name()))
		if err != nil {
			utils.PrintError(fmt.Sprintf("could not read %s: %v", entry.Name(), err))
			os.Exit(1)
		}
		reports = append(reports, report.ToolReport{
			Tool: strings.TrimSuffix(entry.Name(), ".json"),
			Data: data,
		})
	}

	merged, err := report.MergeCodeQuality(reports, qaPathPrefix(engine))
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		utils.PrintError(fmt.Sprintf("could not create the report directory: %v", err))
		os.Exit(1)
	}
	if err := os.WriteFile(reportPath, merged.Data, 0o644); err != nil {
		utils.PrintError(fmt.Sprintf("could not write the report: %v", err))
		os.Exit(1)
	}

	printReportSummary(merged.Counts, reportPath)

	if reportStatusFailed(rawDir) {
		utils.PrintError("QA tools reported errors or warnings.")
		os.Exit(1)
	}
	utils.PrintSuccess("All selected QA tools passed!")
}

// qaPathPrefix is how a reported path is turned into one GitLab can resolve.
//
// On the Dagger engine the repository root is above the application whenever deploy.source_dir is
// set, so that directory has to be prefixed back. On the compose engine the command runs from
// inside the project and the two roots coincide.
func qaPathPrefix(engine string) report.PathPrefix {
	prefix := report.PathPrefix{ContainerRoot: config.GetSourceRootContainerPath()}
	if engine == engineDagger {
		prefix.ContainerRoot = config.OroRootDir
		prefix.RepoSubdir = viper.GetString("deploy.source_dir")
	}
	return prefix
}
