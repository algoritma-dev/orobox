package report

import (
	"sort"
	"strings"
)

// Finding is one violation flattened to what a single output line needs.
//
// It is a struct rather than the map[string]any the merge carries because this is the end of the
// road: nothing downstream re-serialises it, so the fields a tool invented and Orobox has no
// opinion about are genuinely not needed here — unlike in MergeCodeQuality, where dropping them
// would corrupt the document GitLab reads.
type Finding struct {
	Tool     string
	Path     string
	Check    string
	Severity string
	Message  string
	Line     int
}

// Findings reads every tool's report into a flat, sorted list that keeps the tool's name.
//
// MergeCodeQuality cannot be reused: it concatenates the issues into one document and keeps only
// per-tool counts, so by the time it returns there is no way to say which tool reported which
// finding — which is the first thing a caller reading one line per finding needs.
func Findings(reports []ToolReport, prefix PathPrefix) ([]Finding, error) {
	var out []Finding

	for _, report := range reports {
		if len(strings.TrimSpace(string(report.Data))) == 0 {
			continue
		}

		issues, err := toolIssues(report)
		if err != nil {
			return nil, err
		}

		for _, issue := range issues {
			rewriteIssuePath(issue, prefix)
			out = append(out, findingFrom(report.Tool, issue))
		}
	}

	// Sorted by location so two runs over an unchanged tree produce identical output, which is
	// what lets a caller diff them.
	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].Path != out[j].Path:
			return out[i].Path < out[j].Path
		case out[i].Line != out[j].Line:
			return out[i].Line < out[j].Line
		default:
			return out[i].Tool < out[j].Tool
		}
	})

	return out, nil
}

// findingFrom reads one CodeClimate issue. Every field is optional: the tools agree on the shape
// but not on which parts of it they fill in, and a finding missing its severity is still a finding
// worth printing.
func findingFrom(tool string, issue map[string]any) Finding {
	f := Finding{Tool: tool, Severity: "info"}

	if s, ok := issue["severity"].(string); ok && s != "" {
		f.Severity = s
	}
	if s, ok := issue["check_name"].(string); ok {
		f.Check = s
	}
	if s, ok := issue["description"].(string); ok {
		f.Message = collapseWhitespace(stripReportingBoilerplate(s))
	}

	location, ok := issue["location"].(map[string]any)
	if !ok {
		return f
	}
	if s, ok := location["path"].(string); ok {
		f.Path = s
	}
	f.Line = issueBeginLine(location)

	return f
}

// issueBeginLine reads the begin line from either shape CodeClimate allows. The captured fixtures use
// `lines`, but the `positions` form is equally valid and a tool upgrade can switch between them.
func issueBeginLine(location map[string]any) int {
	if lines, ok := location["lines"].(map[string]any); ok {
		if begin, ok := lines["begin"].(float64); ok {
			return int(begin)
		}
	}
	positions, ok := location["positions"].(map[string]any)
	if !ok {
		return 0
	}
	begin, ok := positions["begin"].(map[string]any)
	if !ok {
		return 0
	}
	if line, ok := begin["line"].(float64); ok {
		return int(line)
	}
	return 0
}

// reportingBoilerplate are the trailing instructions a tool appends to a message for a human who
// is about to file a bug against the tool itself.
//
// PHPStan adds two lines to every internal error — the invitation to re-run with -v and the issue
// tracker URL — which together are longer than most of the errors they follow. They are the same
// text every time, so an automated caller pays for them once per finding and learns nothing.
// Cutting from the marker to the end of the message is deliberate: what follows is never part of
// what the tool found.
var reportingBoilerplate = []string{
	"Run PHPStan with -v option and post the stack trace to:",
}

// stripReportingBoilerplate removes a trailing bug-report invitation, leaving the finding itself.
func stripReportingBoilerplate(message string) string {
	for _, marker := range reportingBoilerplate {
		if i := strings.Index(message, marker); i != -1 {
			message = message[:i]
		}
	}
	return message
}

// collapseWhitespace folds a message onto one line.
//
// PHPStan in particular wraps long messages, and a finding that spans three lines breaks the one
// finding, one line contract the agent-mode grammar rests on.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
