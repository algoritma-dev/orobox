//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/scaffold"
)

// TestMain owns the lifetime of the binary the suite builds for itself: it has to outlive
// every subtest that uses it, so it is only removed once the whole run is over.
func TestMain(m *testing.M) {
	code := m.Run()
	cleanupBuiltBinary()
	os.Exit(code)
}

// TestBuiltBinaryOutlivesSubtests guards the harness bug where the binary was built into the
// first subtest's t.TempDir(): Go deleted that directory as soon as the subtest ended, so
// every later matrix case tried to exec a file that no longer existed and failed in
// milliseconds with exit -1 and empty stdout/stderr — a failure that looked like a broken
// CLI rather than a broken harness.
func TestBuiltBinaryOutlivesSubtests(t *testing.T) {
	if _, needBuild := ResolveBinary(os.Getenv); !needBuild {
		t.Skip("OROBOX_BIN is set, so the suite uses that binary instead of building one")
	}

	var bin string
	t.Run("build", func(t *testing.T) {
		bin = binaryPath(t)
		if bin == "" {
			t.Fatal("binaryPath returned an empty path")
		}
	})

	// By now the subtest above has finished and its temp dir is gone.
	t.Run("survives", func(t *testing.T) {
		if bin == "" {
			t.Skip("nothing was built")
		}
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("binary vanished once the subtest that built it ended: %v", err)
		}
	})
}

// TestRunTimeoutDoesNotHangOnOrphanedChildren guards the harness hang where a deadline
// killed orobox but not the `docker compose logs -f` it had spawned: the orphan kept the
// inherited output pipes open, so cmd.Run() waited for EOF indefinitely and the suite sat on
// the `logs` step until the whole -timeout expired.
//
// The stand-in is a shell that leaves a long-lived background child holding the pipes, which
// is exactly the shape of the real failure.
func TestRunTimeoutDoesNotHangOnOrphanedChildren(t *testing.T) {
	box := &Box{t: t, dir: t.TempDir(), bin: "/bin/sh"}

	start := time.Now()
	box.RunTimeout(2*time.Second, "-c", "sleep 600 & sleep 600")
	elapsed := time.Since(start)

	// Deadline (2s) plus the WaitDelay backstop (10s), with room to spare. Without the
	// process-group kill this takes the full 600s.
	if elapsed > 30*time.Second {
		t.Fatalf("RunTimeout blocked for %s: an orphaned child is still holding the output pipes", elapsed)
	}
}

func TestMatrix(t *testing.T) {
	cases, err := ParseMatrix(os.Getenv("E2E_VERSIONS"), os.Getenv("E2E_TYPES"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.Type)+"-"+c.Version, func(t *testing.T) {
			// Serial by default: heavy Docker installs. Opt into parallelism later.
			runGreenPath(t, c)
		})
	}
}

func runGreenPath(t *testing.T, c Case) {
	box := NewBox(t, c)

	// 1. init — clone + composer install + oro:install (longest step).
	initArgs := []string{"init", "-t", string(c.Type), "-v", c.Version}
	if c.Type == TypeBundle {
		initArgs = append(initArgs, "-n", `Orobox\Bundle\E2ETestBundle\E2ETestBundle`)
	}
	box.Run(initArgs...)

	// 2. up — stack must serve HTTP 200 on storefront and /admin.
	box.Run("up")
	base := c.BaseURL(os.Getenv)
	box.AssertHTTP200(base)
	box.AssertHTTP200(base + "/admin")

	// 3. run — a custom command defined in the fixture's `commands:` section. `orobox run`
	// dispatches those by name; it is not a console passthrough (that is `console`, below),
	// so passing a bare Symfony command here only ever yields
	// "command '...' not found in .orobox.yaml".
	box.Run("run", e2eRunCommand)

	// 4. console — non-interactive read-only command.
	box.Run("console", "cache:pool:list")

	// 5. db backup + restore round-trip (both install types).
	dump := filepath.Join(box.Dir(), "backup.sql")
	box.Run("db", "backup", dump)
	if fi, err := os.Stat(dump); err != nil || fi.Size() == 0 {
		t.Fatalf("db backup produced no file: err=%v", err)
	}
	box.Run("db", "restore", dump)

	// 6. logs (follow-mode: bounded by timeout, kill is expected), xdebug lifecycle.
	if res := box.RunTimeout(5*time.Second, "logs", "--nginx"); res.Stdout == "" && res.Stderr == "" {
		t.Logf("logs produced no output for %s %s", c.Type, c.Version)
	}
	box.Run("xdebug", "status")
	box.Run("xdebug", "on")
	box.Run("xdebug", "off")

	// 7. generators — assert they wrote something into the checkout.
	//
	// deploy-init and ci-init are project-only by design: a bundle checkout is not a
	// deployable application, and its CI would have to stand up a full development stack per
	// job. For bundle, assert the refusal instead of generated files, so the guard itself
	// stays covered.
	for _, gen := range projectOnlyGenerators {
		if c.Type == TypeProject {
			assertGenerator(t, box, gen)
			continue
		}
		if res := box.TryRun(gen); !failed(res) {
			t.Errorf("%s should refuse install type %q, got exit %d\n%s", gen, c.Type, res.ExitCode, res.Stdout)
		}
	}
	// qa-init's stubs are asserted by name rather than by a file count: for a bundle the
	// checkout also holds vendor-oro (bind-mounted as the container's vendor), and the QA
	// install rewrites that tree, so the total file count can fall even though every stub
	// was written.
	//
	// The expected paths come from the scaffold itself so they cannot drift from the
	// implementation. QaStubs consults config.IsQaToolEnabled, which defaults to true for
	// keys no config sets — true here, since the fixtures disable no QA tool. A fixture that
	// starts disabling one would have to pass that config in as well.
	assertGeneratedFiles(t, box, "qa-init", scaffoldRelPaths(scaffold.QaStubs(string(c.Type))))

	// 8. qa — deliberately after qa-init, and graded per tool rather than on the exit code.
	//
	// The order is what makes the step mean anything for a bundle: `orobox qa` on the compose
	// engine refuses to run tools that are not installed, so run before qa-init it only ever
	// exercised that refusal. The project matrix takes the pipeline engine, which installs its
	// own tools and never needed the ordering — but it did need the assert: qa was best-effort
	// here, so every version silently failed its Rector step on OroCommerce's generated kernel
	// while the job stayed green.
	assertQa(t, box, c)

	// 9. test-init is not a generator: it provisions the test database and cache (its only
	// write to the checkout is .orobox.yaml, and only behind --tmpfs), so counting files
	// would always report "created no new files". Assert that it completes instead. It runs
	// a full oro:install --env=test, so this is one of the slower steps.
	box.Run("test-init")

	// 10. test — narrowed, and deliberately after test-init: while the test database is
	// missing, `orobox test` prints "run 'orobox test-init'" and returns without ever
	// invoking PHPUnit, so run any earlier the step asserts nothing.
	assertNarrowedTests(t, box, c)

	// 11. clear + down (teardown also runs in cleanup). The command is "clear", not
	// "clean": it removes every container and volume so the next run starts fresh.
	box.Run("clear")
	box.Run("down")
}

// assertQa drives `orobox qa` in report mode and grades each tool on what it recorded, not on the
// command's exit code.
//
// What the step is here to prove is that every configured tool is installed, configured and
// actually invoked against a real Oro checkout. Whether that analysis is clean is a property of
// Oro's own code and of the tools' rulesets — PHPStan has findings on stock OroCommerce in some
// versions — so findings are logged and tolerated. A tool that could not run is not tolerated:
// that is the installation or configuration bug this step exists to catch.
//
// Report mode is also what keeps the later tools running at all: `orobox qa` without --report
// chains the tools with &&, so the first one with findings silences every tool after it, and the
// suite learned nothing about Rector or PHP-CS-Fixer whenever PHPStan had something to say.
func assertQa(t *testing.T, box *Box, c Case) {
	t.Helper()

	// The raw directory is the input to the grading, and the tools only overwrite the files they
	// write themselves, so a leftover status from an earlier step would be graded as this run's.
	// Best-effort: the container may own the files, and the assertions below still catch a run
	// that produced nothing.
	rawDir := filepath.Join(box.Dir(), config.RawReportsRelDir, "qa")
	if err := os.RemoveAll(rawDir); err != nil {
		t.Logf("could not clear %s before the qa step: %v", rawDir, err)
	}

	reportRel := filepath.Join(config.ReportsRelDir, "code-quality.json")
	res := box.TryRun("qa", "--report", "gitlab", "--report-path", reportRel)

	// In report mode the command exits non-zero for findings too, so the exit code alone cannot
	// fail the step. What it does mean is that the run has to have got far enough to write the
	// merged report: a missing binary, an engine that could not start, or a tool whose output was
	// not a Code Quality document all stop the command before this file exists.
	if _, err := os.ReadFile(filepath.Join(box.Dir(), reportRel)); err != nil {
		t.Errorf("qa wrote no code quality report for %s %s: exit %d, %v\n%s\n%s",
			c.Type, c.Version, res.ExitCode, err, res.Stdout, res.Stderr)
		return
	}

	outcomes, err := ReadQaOutcomes(rawDir)
	if err != nil {
		t.Errorf("qa left no readable per-tool results for %s %s: %v\n%s\n%s",
			c.Type, c.Version, err, res.Stdout, res.Stderr)
		return
	}
	if len(outcomes) == 0 {
		t.Errorf("qa invoked no tool for %s %s: exit %d\n%s\n%s",
			c.Type, c.Version, res.ExitCode, res.Stdout, res.Stderr)
		return
	}

	for _, outcome := range outcomes {
		switch {
		case outcome.Broken():
			t.Errorf("qa tool %s could not run for %s %s: exit %d, %d findings, report error %v\n%s\n%s",
				outcome.Tool, c.Type, c.Version, outcome.ExitCode, outcome.Findings, outcome.ReportErr,
				res.Stdout, res.Stderr)
		case outcome.Lint():
			t.Logf("qa tool %s reported %d findings (tolerated)", outcome.Tool, outcome.Findings)
		default:
			t.Logf("qa tool %s passed", outcome.Tool)
		}
	}
}

// assertNarrowedTests drives `orobox test` once per gated suite, each run narrowed by
// e2eTestFilter, and grades it from the JUnit report rather than from the exit code.
//
// Grading follows the suite's contract for `test`: a community install that cannot run the
// current PHPUnit tooling at all is logged and skipped, because that is a property of the
// install and not of orobox. What is not tolerated is a run that claims success without
// executing a test — PHPUnit exits 0 on "No tests executed", so the report's counts are the
// only thing that separates the two — nor tests that ran and did not pass.
func assertNarrowedTests(t *testing.T, box *Box, c Case) {
	t.Helper()

	for _, suite := range e2eTestSuites {
		reportRel := filepath.Join(config.ReportsRelDir, "junit-"+suite+".xml")

		// Every run writes its raw log to the same reports/raw/test/junit.xml, and the report is
		// merged from whatever that directory holds. Clearing it first is what keeps a suite that
		// crashed before PHPUnit wrote anything from being graded on the previous suite's counts.
		// Best-effort: the container may own the file, and a stale-file misgrade is a smaller
		// problem than a case that cannot run at all.
		rawDir := filepath.Join(box.Dir(), config.RawReportsRelDir, "test")
		if err := os.RemoveAll(rawDir); err != nil {
			t.Logf("could not clear %s before the %s suite: %v", rawDir, suite, err)
		}

		res := box.TryRun("test",
			"--testsuite", suite,
			"--filter", c.TestFilter(),
			"--report", "gitlab",
			"--report-path", reportRel)

		// In report mode a failing suite still writes the report and then exits nonzero, so the
		// report is read before the exit code is judged.
		data, err := os.ReadFile(filepath.Join(box.Dir(), reportRel))
		if err != nil {
			if failed(res) {
				t.Logf("test --testsuite %s skipped/failed (best-effort) for %s %s: exit %d\n%s\n%s",
					suite, c.Type, c.Version, res.ExitCode, res.Stdout, res.Stderr)
				continue
			}
			t.Errorf("test --testsuite %s exited 0 but wrote no report at %s: %v", suite, reportRel, err)
			continue
		}

		totals, parseErr := parseJUnitTotals(data)
		switch {
		case parseErr != nil:
			t.Errorf("test --testsuite %s wrote an unreadable report: %v", suite, parseErr)
		case totals.Tests == 0 && failed(res):
			t.Logf("test --testsuite %s skipped/failed (best-effort) for %s %s: exit %d\n%s\n%s",
				suite, c.Type, c.Version, res.ExitCode, res.Stdout, res.Stderr)
		case totals.Tests == 0:
			t.Errorf("test --testsuite %s executed no test: filter %q matched nothing in %s %s",
				suite, c.TestFilter(), c.Type, c.Version)
		case totals.Failures+totals.Errors > 0:
			t.Errorf("test --testsuite %s ran %d tests with %d failures and %d errors\n%s\n%s",
				suite, totals.Tests, totals.Failures, totals.Errors, res.Stdout, res.Stderr)
		default:
			t.Logf("test --testsuite %s ran %d tests, all green", suite, totals.Tests)
		}
	}
}

// assertGenerator runs a generator command and asserts it created at least one file
// beyond what was already present.
//
// Only safe where the command's output is the only thing changing in the checkout. Prefer
// assertGeneratedFiles when the exact artifacts are known: a bundle checkout also contains
// the vendor-oro tree, which other steps rewrite, so a net count can mislead in both
// directions.
func assertGenerator(t *testing.T, box *Box, cmd string) {
	before := countFiles(t, box.Dir())
	box.Run(cmd)
	if countFiles(t, box.Dir()) <= before {
		t.Fatalf("%s created no new files", cmd)
	}
}

// assertGeneratedFiles runs a generator and asserts each expected path exists and is not
// empty, which does not care what else in the checkout changed.
func assertGeneratedFiles(t *testing.T, box *Box, cmd string, expected []string) {
	t.Helper()
	if len(expected) == 0 {
		t.Fatalf("no expected artifacts declared for %s", cmd)
	}
	box.Run(cmd)
	for _, rel := range expected {
		path := filepath.Join(box.Dir(), rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s did not write %s: %v", cmd, rel, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s wrote an empty %s", cmd, rel)
		}
	}
}

// scaffoldRelPaths lists the paths a set of scaffold artifacts lands on.
func scaffoldRelPaths(artifacts []scaffold.Artifact) []string {
	paths := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		paths = append(paths, a.RelPath)
	}
	return paths
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
