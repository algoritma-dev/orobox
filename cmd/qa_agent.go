package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
)

// appliedFixReporters are the tools whose report, in fix mode, lists what they rewrote rather than
// what is left to fix.
//
// The distinction decides whether a finding fails the run. A file PHP-CS-Fixer already reformatted
// is not a problem the caller has to act on, and failing the command over it would make a passing
// tree unreachable: the next run would report the same file as fixed again only if it had been
// edited again.
//
// twig-cs-fixer is deliberately absent. Its --fix --report=gitlab behaviour has not been verified
// against the real tool, and the safe default for an unknown tool is to list its findings: a wrong
// guess then shows up as noise the developer can see, rather than as findings silently swallowed.
var appliedFixReporters = map[string]bool{
	"php-cs-fixer": true,
	"rector":       true,
}

// qaAgentResult is a formatted agent-mode run, kept separate from printing it so the grammar can
// be tested: every failure path in cmd/qa.go ends in os.Exit, which a test cannot survive.
type qaAgentResult struct {
	Stdout string
	Errors []string
	Failed bool
}

// qaAgentResultFrom turns the per-tool reports and their recorded exit codes into what the run
// prints.
//
// mode matters because the same tool means different things by the same document: in fix mode
// PHP-CS-Fixer's report lists the files it rewrote, in check mode the ones it would have.
func qaAgentResultFrom(reports []report.ToolReport, statuses map[string]int, prefix report.PathPrefix, mode qatools.Mode) (qaAgentResult, error) {
	var result qaAgentResult
	var listed []report.ToolReport
	fixed := map[string]int{}

	for _, r := range reports {
		// A tool that exited non-zero without writing findings did not fail the tree, it failed to
		// run — a missing binary, a broken config, a PHP fatal. Reported as an error, because the
		// caller's fix is not in the code under analysis.
		if status, ok := statuses[r.Tool]; ok && status != 0 && len(strings.TrimSpace(string(r.Data))) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s could not run (exit %d)", r.Tool, status))
			result.Failed = true
			continue
		}

		if mode == qatools.ModeFix && appliedFixReporters[r.Tool] {
			applied, err := report.Findings([]report.ToolReport{r}, prefix)
			if err != nil {
				return qaAgentResult{}, err
			}
			fixed[r.Tool] = len(applied)
			continue
		}

		listed = append(listed, r)
	}

	findings, err := report.Findings(listed, prefix)
	if err != nil {
		return qaAgentResult{}, err
	}
	if len(findings) > 0 {
		result.Failed = true
	}

	result.Stdout = report.FormatAgent(findings, fixed, report.AgentFindingLimit)
	return result, nil
}

// readRawReports reads a report-mode step's untouched per-tool files.
//
// Shared with finishQaReport rather than duplicated: a tool whose report one path skipped and the
// other read would be invisible in exactly one output mode, which is the hardest kind of gap to
// notice.
func readRawReports(rawDir string) ([]report.ToolReport, []string, error) {
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read the raw reports in %s: %v", rawDir, err)
	}

	var reports []report.ToolReport
	var tools []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rawDir, entry.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("could not read %s: %v", entry.Name(), err)
		}
		tool := strings.TrimSuffix(entry.Name(), ".json")
		tools = append(tools, tool)
		reports = append(reports, report.ToolReport{Tool: tool, Data: data})
	}
	return reports, tools, nil
}

// readToolStatuses reads the exit code ReportScript recorded for each tool. A missing file is not
// an error: it means the script never reached that tool, which the aggregate status already says.
func readToolStatuses(rawDir string, tools []string) map[string]int {
	statuses := map[string]int{}
	for _, tool := range tools {
		data, err := os.ReadFile(filepath.Join(rawDir, qatools.ToolStatusFile(tool)))
		if err != nil {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		statuses[tool] = code
	}
	return statuses
}

// finishQaAgent prints an agent-mode QA run and exits with its verdict.
//
// captured is the container's own stdout, which agent mode swallows: ReportScript echoes a header
// per tool and the JS linters print their human output there while writing JSON to a file (see
// internal/report/testdata/NOTES.md). It is written to stderr only when the engine itself failed,
// where it is the only diagnostic there is.
func finishQaAgent(rawDir, engine string, mode qatools.Mode, runErr error, captured []byte) {
	if runErr != nil {
		if len(captured) > 0 {
			output.Err(strings.TrimSpace(string(captured)))
		}
		output.Err(runErr.Error())
		os.Exit(1)
	}

	reports, tools, err := readRawReports(rawDir)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	result, err := qaAgentResultFrom(reports, readToolStatuses(rawDir, tools), qaPathPrefix(engine), mode)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	if result.Stdout != "" {
		fmt.Fprint(output.Payload(), result.Stdout)
	}
	for _, msg := range result.Errors {
		output.Err(msg)
	}

	if result.Failed {
		os.Exit(1)
	}
}
