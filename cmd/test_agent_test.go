package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
)

func splitAgentLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func TestFormatTestAgent(t *testing.T) {
	failures := []report.TestFailure{
		{Suite: "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest", Test: "testTotal", Kind: "failure", Message: "Failed asserting that 2 matches expected 3.", File: "/var/www/oro/src/Acme/Tests/Unit/OrderTest.php", Line: 88},
	}
	counts := report.TestCounts{Passed: 7, Failed: 1}

	want := "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest::testTotal failure Failed asserting that 2 matches expected 3.\n" +
		"7 passed, 1 failed, 0 errors\n"

	if got := formatTestAgent(failures, counts, report.AgentFindingLimit); got != want {
		t.Errorf("formatTestAgent() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatTestAgentPrintsOnlyTheTallyForAPassingRun(t *testing.T) {
	want := "12 passed, 0 failed, 0 errors\n"
	if got := formatTestAgent(nil, report.TestCounts{Passed: 12}, report.AgentFindingLimit); got != want {
		t.Errorf("formatTestAgent() = %q, want %q", got, want)
	}
}

func TestFormatTestAgentTruncates(t *testing.T) {
	var failures []report.TestFailure
	for i := 0; i < 52; i++ {
		failures = append(failures, report.TestFailure{Suite: "S", Test: "t", Kind: "failure", Message: "m"})
	}

	got := formatTestAgent(failures, report.TestCounts{Failed: 52}, 50)

	lines := splitAgentLines(got)
	if len(lines) != 52 {
		t.Fatalf("produced %d lines, want 52 (50 failures, the truncation line and the tally)", len(lines))
	}
	if want := "... 2 more"; lines[50] != want {
		t.Errorf("line 51 = %q, want %q", lines[50], want)
	}
}

// TestTestAgentForcesTheJUnitLogAndPrintsOnlyTheTally is the wiring guard: agent mode has to ask
// PHPUnit for the machine-readable log even when the caller did not, and must not leave it behind.
func TestTestAgentForcesTheJUnitLogAndPrintsOnlyTheTally(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		output.SetAgent(false)
		docker.ResetEnsuredServices()
	})

	root := agentProject(t)
	rawDir := filepath.Join(root, config.RawReportsRelDir, "test")

	var args []string
	var streamed bool
	docker.RunComposeCommand = func(_ string, _ ...string) error {
		streamed = true
		return nil
	}
	docker.RunComposeCommandSilently = docker.RunComposeCommand
	docker.RunComposeCommandWithOutput = func(a ...string) ([]byte, error) {
		if len(a) > 0 && a[0] == "ps" {
			return psRunningRequested(a), nil
		}
		// The database probe is an `exec` too, so the PHPUnit run is told apart by its own
		// arguments rather than by the subcommand.
		if contains(a, "psql") {
			return []byte("1"), nil
		}
		if len(a) > 0 && a[0] == "exec" {
			args = a
			if err := os.MkdirAll(rawDir, 0o755); err != nil {
				t.Fatal(err)
			}
			doc := `<?xml version="1.0"?><testsuites><testsuite name="Unit" tests="2">` +
				`<testcase name="testA" class="C"/><testcase name="testB" class="C"/>` +
				`</testsuite></testsuites>`
			if err := os.WriteFile(filepath.Join(rawDir, "junit.xml"), []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			return []byte("PHPUnit 9.6.0 by Sebastian Bergmann.\n..\n"), nil
		}
		return []byte("[]"), nil
	}

	var payload, errs bytes.Buffer
	restore := output.SetWriters(&payload, &errs)
	t.Cleanup(restore)
	output.SetAgent(true)

	runTestOnCompose(qatools.ReportNone)

	if len(args) == 0 {
		t.Fatal("the agent-mode run never reached the container through the capturing runner")
	}
	if streamed {
		t.Error("the agent-mode run used the streaming runner, so PHPUnit's own output reached stdout")
	}
	if !contains(args, "--log-junit") {
		t.Errorf("agent mode did not ask PHPUnit for a JUnit log: %v", args)
	}
	if got, want := payload.String(), "2 passed, 0 failed, 0 errors\n"; got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
	if errs.Len() != 0 {
		t.Errorf("a passing agent-mode run wrote %q to stderr, want nothing", errs.String())
	}
	if _, err := os.Stat(rawDir); !os.IsNotExist(err) {
		t.Errorf("the raw report directory survived a run that did not ask for a report: %v", err)
	}
}
