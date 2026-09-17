package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/report"
)

// formatTestAgent renders a PHPUnit run as the lines an automated caller reads: one per failing
// test, then the tally.
//
// The tally is printed even for a passing run, unlike the QA formatter's silence. A test run that
// reported nothing because it executed no tests is a different outcome from one that passed, and
// the exit code cannot tell the two apart.
func formatTestAgent(failures []report.TestFailure, counts report.TestCounts, limit int) string {
	var b strings.Builder

	shown := failures
	if limit > 0 && len(failures) > limit {
		shown = failures[:limit]
	}
	for _, f := range shown {
		fmt.Fprintf(&b, "%s::%s %s %s\n", f.Suite, f.Test, f.Kind, f.Message)
	}
	if dropped := len(failures) - len(shown); dropped > 0 {
		fmt.Fprintf(&b, "... %d more\n", dropped)
	}

	fmt.Fprintf(&b, "%d passed, %d failed, %d errors\n", counts.Passed, counts.Failed, counts.Errored)
	return b.String()
}

// finishTestAgent prints an agent-mode test run and exits with its verdict.
//
// runErr is PHPUnit's own non-zero exit on a failing suite, which is not a reason to print the
// captured output: the failures are in the JUnit log and printing both would double the cost. It
// is only surfaced when the log says nothing, which means the run never reached PHPUnit.
func finishTestAgent(rawDir string, runErr error, captured []byte) {
	doc, readErr := os.ReadFile(filepath.Join(rawDir, "junit.xml"))
	if readErr != nil {
		if len(captured) > 0 {
			output.Err(strings.TrimSpace(string(captured)))
		}
		output.Err(fmt.Sprintf("the test run wrote no JUnit log: %v", readErr))
		os.Exit(1)
	}

	failures, counts, err := report.FailuresFromJUnit(doc)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	fmt.Fprint(output.Payload(), formatTestAgent(failures, counts, report.AgentFindingLimit))

	if len(failures) > 0 || runErr != nil {
		os.Exit(1)
	}
}
