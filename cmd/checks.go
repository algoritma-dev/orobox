// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/viper"
)

// The two engines `orobox qa` and `orobox test` can run on. compose is the development stack the
// commands have always used; dagger is the deploy pipeline's own engine, which brings its layer
// cache, its database dump and its fingerprints with it.
const (
	engineCompose = "compose"
	engineDagger  = "dagger"
)

// reportsRelDir is where the reports live, relative to the project root. The value lives in
// internal/config because the generated GitLab pipeline names the very same paths.
const reportsRelDir = config.ReportsRelDir

// rawReportsRelDir holds the per-tool files exactly as the tools wrote them. They are kept rather
// than deleted after the merge: when a merged report looks wrong, the tool's own output is the
// only thing that says whether the tool or the merge is to blame.
const rawReportsRelDir = config.RawReportsRelDir

// resolveEngine picks where the checks run.
//
// The default is the Dagger engine in CI and the compose stack elsewhere, because that is where
// each is right: a CI job has no development stack and would pay for a full composer install and a
// full oro:install per run, while a developer's laptop has the stack already up and does not want
// an engine started behind its back.
//
// The install type is part of the decision because the pipeline image only exists for projects
// (algoritmadev/orobox:<version>-project-latest). An explicit --engine=dagger on any other type is
// an error rather than a silent fallback: the user asked for something that cannot be delivered.
func resolveEngine(flag string) (string, error) {
	installType := viper.GetString("type")
	isProject := installType == config.InstallTypeProject

	switch flag {
	case engineCompose:
		return engineCompose, nil
	case engineDagger:
		if !isProject {
			return "", fmt.Errorf("--engine=%s is only available for install type %q; this project is %q, and the pipeline image exists only for projects",
				engineDagger, config.InstallTypeProject, installType)
		}
		return engineDagger, nil
	case "":
		if os.Getenv("CI") != "" && isProject {
			return engineDagger, nil
		}
		return engineCompose, nil
	default:
		return "", fmt.Errorf("unknown engine %q: use %s or %s", flag, engineCompose, engineDagger)
	}
}

// qaTestEnvInstalled reports whether the test database this stack would analyse against is
// actually installed. It is a variable so the environment resolution can be tested without a
// running stack.
var qaTestEnvInstalled = func() bool {
	installed, err := docker.IsDatabaseInitialized(true)
	// An error here is the db-test service not being up at all, which is exactly the state that
	// must not resolve to the test environment.
	return err == nil && installed
}

// resolveQaEnv picks the Symfony environment the QA tools boot on the compose engine.
//
// A developer's machine gets dev, because that is what `orobox install` leaves running; asking for
// test there would warm a cache against a database that only exists after `orobox test-init` and
// would compete with the functional tests for it.
//
// CI gets test *if the test database is installed*, which is the state a CI job reaches by running
// `orobox test-init` before the checks. The env var alone is not enough: PHPStan's setup warms the
// cache of whichever environment it is given, and warming test queries Oro's config tables, so
// pointing it at an environment that was never installed fails the tool with "could not translate
// host name db-test to address" — a broken tool rather than a verdict about the code. The check
// runs for both `orobox qa-init` (which bakes the environment into the generated phpstan.neon) and
// `orobox qa`, so the two cannot disagree about which cache is being read.
//
// The Dagger engine is not routed through here: it installs its own test database whether or not
// it was started from CI, so pipeline.NewChecks stays on qatools.EnvTest.
func resolveQaEnv() qatools.Env {
	if os.Getenv("CI") == "" {
		return qatools.EnvDev
	}
	if !qaTestEnvInstalled() {
		utils.PrintWarning("The test database is not installed, so the QA tools run in the dev environment. Run 'orobox test-init' first to analyse against test.")
		return qatools.EnvDev
	}
	return qatools.EnvTest
}

// resolveReport parses the --report value. Only GitLab's format is implemented; the flag exists as
// an enum so another one can be added without changing the command's surface.
func resolveReport(flag string) (qatools.Report, error) {
	switch flag {
	case "":
		return qatools.ReportNone, nil
	case "gitlab":
		return qatools.ReportGitLab, nil
	default:
		return qatools.ReportNone, fmt.Errorf("unknown report format %q: the only accepted value is gitlab", flag)
	}
}

// resolveCacheScope names the cache volume family for a Dagger run.
//
// The deploy pipeline scopes its volumes by git ref; a check has no ref, so the branch takes its
// place. That keeps two branches with different migrations from invalidating each other's
// fingerprint, and it puts a lint job on the same volumes the deploy of that branch uses.
func resolveCacheScope(flag string) string {
	if flag != "" {
		return flag
	}
	if branch := os.Getenv("CI_COMMIT_REF_NAME"); branch != "" {
		return branch
	}
	if branch, err := currentGitBranch(); err == nil && branch != "" {
		return branch
	}
	return "orobox-checks"
}

// currentGitBranch is the local fallback for the cache scope. It shells out rather than pulling in
// a git library: this and the deploy's HEAD lookup are the only git calls the CLI makes on the
// host, and a dependency for two command lines would not pay for itself.
func currentGitBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = config.GetHostBundlePath()

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// resolveReportPath turns the --report-path flag into an absolute host path.
//
// The path must stay inside the project, and not for tidiness: on the compose engine the tools
// write the report through the bind mount of the source root, so a destination outside the project
// is one the container cannot reach. Rejecting it here gives a clear message instead of a file that
// never appears.
func resolveReportPath(flag, defaultRelPath string) (string, error) {
	projectDir := config.GetHostBundlePath()

	relative := flag
	if relative == "" {
		relative = defaultRelPath
	}

	absolute := relative
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(projectDir, relative)
	}

	cleaned := filepath.Clean(absolute)
	if !strings.HasPrefix(cleaned, filepath.Clean(projectDir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("--report-path must point inside the project (%s): the container writes it through the project's bind mount", projectDir)
	}
	return cleaned, nil
}

// rawReportDir is the host directory holding one step's untouched per-tool files. kind is "qa" or
// "test".
func rawReportDir(kind string) string {
	return filepath.Join(config.GetHostBundlePath(), rawReportsRelDir, kind)
}

// reportStatusFailed reads the status a report-mode step recorded instead of failing.
//
// A missing file is not a failure: it means the step never got as far as writing one, and the
// caller has a real error to report about that. Only an explicit non-zero value is a failure.
func reportStatusFailed(rawDir string) bool {
	data, err := os.ReadFile(filepath.Join(rawDir, qatools.StatusFile))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) != "0"
}

// printReportSummary tells the user what the report holds. Report mode redirects the PHP tools'
// stdout into files, so without this a run that found violations would say almost nothing.
func printReportSummary(counts map[string]int, path string) {
	total := 0
	for tool, count := range counts {
		total += count
		if count > 0 {
			utils.PrintWarning(fmt.Sprintf("  %s: %d", tool, count))
		}
	}
	if total == 0 {
		utils.PrintSuccess("No violations reported.")
	}
	utils.PrintInfo("Report written to " + path)
}
