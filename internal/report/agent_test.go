package report

import (
	"strings"
	"testing"
)

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func TestFormatAgentGrammar(t *testing.T) {
	findings := []Finding{
		{Tool: "phpstan", Path: "src/Entity/Order.php", Line: 42, Severity: "error", Message: "Access to undefined property App\\Entity\\Order::$foo"},
		{Tool: "eslint", Path: "src/Form/Type/OrderType.php", Line: 11, Check: "semi", Severity: "major", Message: "Missing semicolon."},
	}
	fixed := map[string]int{"php-cs-fixer": 5, "rector": 2}

	want := "fixed 7 files (php-cs-fixer 5, rector 2)\n" +
		"src/Entity/Order.php:42 error phpstan Access to undefined property App\\Entity\\Order::$foo\n" +
		"src/Form/Type/OrderType.php:11 major eslint semi Missing semicolon.\n"

	if got := FormatAgent(findings, fixed, AgentFindingLimit); got != want {
		t.Errorf("FormatAgent() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatAgentPrintsNothingForACleanRun(t *testing.T) {
	if got := FormatAgent(nil, nil, AgentFindingLimit); got != "" {
		t.Errorf("FormatAgent() = %q, want the empty string", got)
	}
	if got := FormatAgent(nil, map[string]int{"rector": 0}, AgentFindingLimit); got != "" {
		t.Errorf("FormatAgent() with only zero counts = %q, want the empty string", got)
	}
}

func TestFormatAgentTruncates(t *testing.T) {
	var findings []Finding
	for i := 0; i < 53; i++ {
		findings = append(findings, Finding{Tool: "phpstan", Path: "a.php", Line: i, Severity: "error", Message: "m"})
	}

	got := FormatAgent(findings, nil, 50)

	lines := splitLines(got)
	if len(lines) != 51 {
		t.Fatalf("FormatAgent() produced %d lines, want 51 (50 findings plus the truncation line)", len(lines))
	}
	if want := "... 3 more (--report=gitlab for the full list)"; lines[50] != want {
		t.Errorf("last line = %q, want %q", lines[50], want)
	}
}

func TestFormatAgentOmitsTheLineNumberWhenTheToolReportedNone(t *testing.T) {
	findings := []Finding{{Tool: "rector", Path: "src/a.php", Severity: "minor", Message: "m"}}

	want := "src/a.php minor rector m\n"
	if got := FormatAgent(findings, nil, AgentFindingLimit); got != want {
		t.Errorf("FormatAgent() = %q, want %q", got, want)
	}
}
