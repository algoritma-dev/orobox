package report

import (
	"fmt"
	"sort"
	"strings"
)

// AgentFindingLimit caps how many findings an agent-mode run prints.
//
// The cap exists because the whole point of the mode is the caller's token budget: a first run
// against an unanalysed tree can report thousands of findings, and an agent cannot act on the
// four-hundredth one before it has fixed the first. The count of what was dropped is printed, so
// nothing is silently lost.
const AgentFindingLimit = 50

// FormatAgent renders a QA run as the lines an automated caller reads.
//
// The grammar is one finding per line, `path:line severity tool [check] message`, preceded by a
// single line counting what the fixers already rewrote. A clean run renders to the empty string
// rather than to a success message: an exit code already says that, and printing it costs the
// caller tokens to learn nothing.
func FormatAgent(findings []Finding, fixed map[string]int, limit int) string {
	var b strings.Builder

	if line := fixedLine(fixed); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}

	shown := findings
	if limit > 0 && len(findings) > limit {
		shown = findings[:limit]
	}
	for _, f := range shown {
		b.WriteString(formatFinding(f))
		b.WriteString("\n")
	}
	if dropped := len(findings) - len(shown); dropped > 0 {
		fmt.Fprintf(&b, "... %d more (--report=gitlab for the full list)\n", dropped)
	}

	return b.String()
}

// fixedLine summarises what the fixers rewrote, or returns "" when they rewrote nothing.
//
// The tools are named with their counts rather than the files listed, because the caller's next
// move is the same whatever the file names are — re-read the tree — and the names would cost a
// line each to say so.
func fixedLine(fixed map[string]int) string {
	total := 0
	tools := make([]string, 0, len(fixed))
	for tool, count := range fixed {
		if count == 0 {
			continue
		}
		total += count
		tools = append(tools, fmt.Sprintf("%s %d", tool, count))
	}
	if total == 0 {
		return ""
	}
	sort.Strings(tools)
	return fmt.Sprintf("fixed %d files (%s)", total, strings.Join(tools, ", "))
}

// formatFinding renders one line. The line number is omitted rather than printed as `:0` when the
// tool reported none: a fabricated line one would send a reader to the wrong place.
func formatFinding(f Finding) string {
	location := f.Path
	if f.Line > 0 {
		location = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}

	parts := []string{location, f.Severity, f.Tool}
	if f.Check != "" {
		parts = append(parts, f.Check)
	}
	if f.Message != "" {
		parts = append(parts, f.Message)
	}
	return strings.Join(parts, " ")
}
