// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/pipeline"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var (
	filter    string
	testsuite string

	testEngine         string
	testReport         string
	testReportPath     string
	testCacheScope     string
	testBaseCacheScope string
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests (PHPUnit)",
	Run: func(_ *cobra.Command, _ []string) {
		docker.SetIncludeTestFiles(true)
		docker.EnsureDockerCompose()
		utils.PrintInfo("Running tests...")
		runTestCommand()
	},
}

func init() {
	testCmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter tests by name")
	testCmd.Flags().StringVarP(&testsuite, "testsuite", "t", "", "Run specific test suite")

	testCmd.Flags().StringVar(&testEngine, "engine", "", "Where the tests run: compose or dagger (default: dagger in CI, compose otherwise)")
	testCmd.Flags().StringVar(&testReport, "report", "", "Emit a machine-readable report: gitlab")
	testCmd.Flags().StringVar(&testReportPath, "report-path", "", "Where to write the report (default: "+reportsRelDir+"/junit.xml)")
	testCmd.Flags().StringVar(&testCacheScope, "cache-scope", "", "Name the cache volume family, dagger engine only (default: the current branch)")
	testCmd.Flags().StringVar(&testBaseCacheScope, "base-cache-scope", "", "Seed a missing test database dump from this cache scope, dagger engine only")

	rootCmd.AddCommand(testCmd)
}

func runTestCommand() {
	engine, err := resolveEngine(testEngine)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}
	format, err := resolveReport(testReport)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	if engine == engineDagger {
		runTestOnDagger(format)
		return
	}
	runTestOnCompose(format)
}

func runTestOnCompose(format qatools.Report) {
	// Ensure test database and application are running
	if err := docker.EnsureServicesRunning([]string{"db-test", "application"}); err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to ensure services are running: %v", err))
	}

	// Check if database is initialized
	utils.StartLoader("Checking test environment...")
	isInstalled, err := docker.IsDatabaseInitialized(true)
	utils.StopLoader()

	if err != nil {
		utils.PrintWarning(fmt.Sprintf("failed to check database status: %v", err))
		// We proceed anyway, PHPUnit will fail later with a better error message if it's really down
	}

	if !isInstalled {
		utils.PrintError("Test database is not initialized.")
		utils.PrintInfo("Please run 'orobox test-init' to prepare the test environment.")
		return
	}

	var args []string

	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	args = append(args, "application")

	installType, err := config.InstallTypeFor(viper.GetString("type"))
	if err != nil {
		installType, _ = config.InstallTypeFor(config.InstallTypeBundle)
	}
	if installType.BindWholeRepo() {
		// Project: phpunit.xml lives at the app root; run from the default workdir.
		args = append(args, "php", "bin/simple-phpunit")
	} else {
		// Bundle: point phpunit at the bundle's source root inside the prebuilt app.
		args = append(args, "./bin/simple-phpunit", "--configuration="+config.GetSourceRootContainerPath())
	}

	if filter != "" {
		args = append(args, "--filter", filter)
	}
	if testsuite != "" {
		args = append(args, "--testsuite", testsuite)
	}

	if format != qatools.ReportNone {
		// The source root is bind-mounted, so the directory created here is the one PHPUnit writes
		// into and the file it produces is already on the host.
		if err := os.MkdirAll(rawReportDir("test"), 0o755); err != nil {
			utils.PrintError(fmt.Sprintf("could not create the report directory: %v", err))
			os.Exit(1)
		}
		args = append(args, "--log-junit",
			config.GetSourceRootContainerPath()+"/"+rawReportsRelDir+"/test/junit.xml")
	}

	err = docker.RunComposeCommand("", args...)

	if format != qatools.ReportNone {
		// The compose engine runs PHPUnit directly rather than through a wrapper script, so a
		// failing suite arrives as an error rather than in a status file. The report is merged
		// either way: a failing run is when it matters most.
		finishTestReport(rawReportDir("test"), err)
		return
	}

	if err != nil {
		utils.PrintError(fmt.Sprintf("Tests reported errors: %v", err))
		os.Exit(1)
	}
	utils.PrintSuccess("Tests completed successfully!")
}

// runTestOnDagger runs the pipeline's test step: the same PHPUnit invocation, against the cached
// Oro install the deploy pipeline maintains for this branch.
func runTestOnDagger(format qatools.Report) {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		utils.PrintError(fmt.Sprintf("error reading config: %v", err))
		os.Exit(1)
	}

	var suites []string
	if testsuite != "" {
		suites = []string{testsuite}
	}

	projectDir := config.GetHostBundlePath()
	plan := pipeline.NewChecks(&conf, pipeline.ChecksOptions{
		ProjectDir:     projectDir,
		CacheScope:     resolveCacheScope(testCacheScope),
		BaseCacheScope: testBaseCacheScope,
		RunTest:        true,
		Suites:         suites,
		Filter:         filter,
		Report:         format,
	})

	utils.PrintInfo("Running the tests in the pipeline engine. The first run has no caches and takes a while.")

	result, runErr := pipeline.Run(context.Background(), plan, pipeline.Options{
		ProjectDir:    projectDir,
		Debug:         viper.GetBool("debug"),
		ReportHostDir: filepath.Join(projectDir, rawReportsRelDir),
	})

	if format != qatools.ReportNone {
		if runErr != nil {
			utils.PrintError(runErr.Error())
			os.Exit(1)
		}
		// In report mode the step exits 0 whatever PHPUnit concluded, so the outcome is in the
		// status file.
		var suiteErr error
		if reportStatusFailed(result.TestReportDir) {
			suiteErr = fmt.Errorf("the test suites reported failures")
		}
		finishTestReport(result.TestReportDir, suiteErr)
		return
	}

	if runErr != nil {
		utils.PrintError(runErr.Error())
		os.Exit(1)
	}
	utils.PrintSuccess("Tests completed successfully!")
}

// finishTestReport merges the per-suite JUnit logs into one document and then reports the run's
// outcome. The merge happens before the failure is raised: a failing suite is exactly the run whose
// report a CI job needs to publish.
func finishTestReport(rawDir string, runErr error) {
	reportPath, err := resolveReportPath(testReportPath, reportsRelDir+"/junit.xml")
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	var docs [][]byte
	if rawDir != "" {
		entries, readErr := os.ReadDir(rawDir)
		if readErr == nil {
			for _, entry := range entries {
				if filepath.Ext(entry.Name()) != ".xml" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(rawDir, entry.Name()))
				if err != nil {
					utils.PrintError(fmt.Sprintf("could not read %s: %v", entry.Name(), err))
					os.Exit(1)
				}
				docs = append(docs, data)
			}
		}
	}

	merged, mergeErr := report.MergeJUnit(docs)
	if mergeErr != nil {
		utils.PrintError(mergeErr.Error())
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		utils.PrintError(fmt.Sprintf("could not create the report directory: %v", err))
		os.Exit(1)
	}
	if err := os.WriteFile(reportPath, merged, 0o644); err != nil {
		utils.PrintError(fmt.Sprintf("could not write the report: %v", err))
		os.Exit(1)
	}
	utils.PrintInfo("Report written to " + reportPath)

	if runErr != nil {
		utils.PrintError(fmt.Sprintf("Tests reported errors: %v", runErr))
		os.Exit(1)
	}
	utils.PrintSuccess("Tests completed successfully!")
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
