package report

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRectorReportIsConvertedFromItsOwnJSON runs the conversion against the captured fixture, which
// is what Rector 2.6 actually writes for `--output-format=json`: a summary object, not the list of
// findings every other tool emits.
func TestRectorReportIsConvertedFromItsOwnJSON(t *testing.T) {
	data, err := os.ReadFile("testdata/rector.json")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	result, err := MergeCodeQuality([]ToolReport{{Tool: "rector", Data: data}}, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("MergeCodeQuality returned %v", err)
	}
	if result.Counts["rector"] != 2 {
		t.Fatalf("Counts[rector] = %d, want the file diff and the system error", result.Counts["rector"])
	}

	var issues []map[string]any
	if err := json.Unmarshal(result.Data, &issues); err != nil {
		t.Fatalf("merged document is not valid JSON: %v", err)
	}

	diff, systemError := issues[0], issues[1]

	// A rule that wants to rewrite the file is a style finding; Rector failing on a file is not.
	if diff["severity"] != "minor" || systemError["severity"] != "blocker" {
		t.Errorf("severities = %v and %v, want minor for the diff and blocker for the system error", diff["severity"], systemError["severity"])
	}
	// The rules are the finding's title in GitLab, and the namespace is noise there.
	description, _ := diff["description"].(string)
	if !strings.Contains(description, "RemoveUnusedVariableAssignRector") || strings.Contains(description, `\`) {
		t.Errorf("description = %q, want the short rule names", description)
	}
	// The diff itself is what makes the annotation actionable.
	content, ok := diff["content"].(map[string]any)
	if !ok || !strings.Contains(content["body"].(string), "@@") {
		t.Errorf("the diff is missing from the finding: %v", diff["content"])
	}
	if got := issueLine(t, diff); got != 1 {
		t.Errorf("diff line = %v, want the 1 its first hunk header names", got)
	}
	if got := issueLine(t, systemError); got != 3 {
		t.Errorf("system error line = %v, want the 3 Rector reported", got)
	}
	// GitLab drops an annotation whose path it cannot resolve against the repository root, and it
	// deduplicates on the fingerprint.
	location := diff["location"].(map[string]any)
	if got := location["path"]; got != "var/qa-probe/Bad.php" {
		t.Errorf("path = %v, want the reported path", got)
	}
	fingerprint, _ := diff["fingerprint"].(string)
	if fingerprint == "" || fingerprint == systemError["fingerprint"] {
		t.Errorf("fingerprints are not distinct: %q and %v", fingerprint, systemError["fingerprint"])
	}
}

// TestRectorFingerprintFollowsTheFinding pins what GitLab uses it for: the same finding across two
// pipelines is one annotation, a changed one is a new annotation.
func TestRectorFingerprintFollowsTheFinding(t *testing.T) {
	document := func(diff string) []byte {
		return []byte(`{"file_diffs":[{"file":"src/A.php","diff":"` + diff + `","applied_rectors":["Rector\\Foo\\BarRector"]}]}`)
	}

	first, err := rectorIssues(document("@@ -3,4 +3,4 @@ one"))
	if err != nil {
		t.Fatalf("rectorIssues returned %v", err)
	}
	same, err := rectorIssues(document("@@ -3,4 +3,4 @@ one"))
	if err != nil {
		t.Fatalf("rectorIssues returned %v", err)
	}
	other, err := rectorIssues(document("@@ -9,4 +9,4 @@ two"))
	if err != nil {
		t.Fatalf("rectorIssues returned %v", err)
	}

	if first[0]["fingerprint"] != same[0]["fingerprint"] {
		t.Error("an unchanged finding changed fingerprint, so GitLab would report it twice")
	}
	if first[0]["fingerprint"] == other[0]["fingerprint"] {
		t.Error("a changed finding kept its fingerprint, so GitLab would hide it")
	}
}

// TestRectorEmptyRunIsNoFindings covers the clean run: Rector still writes a document, and it must
// not turn into an annotation.
func TestRectorEmptyRunIsNoFindings(t *testing.T) {
	issues, err := rectorIssues([]byte(`{"totals":{"changed_files":0,"errors":0}}`))
	if err != nil {
		t.Fatalf("rectorIssues returned %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("a clean run produced %d findings: %v", len(issues), issues)
	}
}

// TestRectorDocumentIsFoundUnderAnyPreamble is the failure this report file keeps producing, in
// both of its shapes: the report is Rector's stdout, and whatever else printed on that stream — a
// Symfony warning block, a PHP `Warning:` from an autoloaded file — arrives in the file ahead of
// the document. The document is located rather than assumed, so a finding is still reported.
func TestRectorDocumentIsFoundUnderAnyPreamble(t *testing.T) {
	document := `{
    "totals": {"changed_files": 1},
    "file_diffs": [
        {"file": "src/A.php", "diff": "@@ -3,4 +3,4 @@", "applied_rectors": ["Rector\\Foo\\BarRector"]}
    ]
}`

	preambles := map[string]string{
		"a Symfony warning block": " [WARNING] The \"gitlab\" output format is deprecated and will be removed in the\n           next minor version.\n\n",
		"a PHP warning":           "Warning: Undefined array key 1 in /var/www/oro/vendor/acme/Thing.php on line 20\n",
		"a deprecation notice":    "PHP Deprecated:  Implicit conversion in /var/www/oro/vendor/acme/Thing.php on line 8\n",
	}

	for name, preamble := range preambles {
		t.Run(name, func(t *testing.T) {
			issues, err := rectorIssues([]byte(preamble + document))
			if err != nil {
				t.Fatalf("rectorIssues returned %v", err)
			}
			if len(issues) != 1 {
				t.Fatalf("found %d findings under %s, want the one the document holds", len(issues), name)
			}
		})
	}

	// Output printed after the document is ignored the same way: the decoder stops at the end of
	// the value.
	issues, err := rectorIssues([]byte(document + "\n [NOTE] 1 file would have changed\n"))
	if err != nil {
		t.Fatalf("rectorIssues returned %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("found %d findings under a trailing note, want 1", len(issues))
	}
}

// TestRectorReportWithNoDocumentIsAnError keeps the guarantee the merge exists for: a report file
// with no document in it at all — a PHP fatal, a usage error — is a failure, not a clean run, and
// the error quotes what was there instead so the next occurrence needs no log archaeology.
func TestRectorReportWithNoDocumentIsAnError(t *testing.T) {
	_, err := MergeCodeQuality([]ToolReport{
		{Tool: "rector", Data: []byte("Warning: something printed here\nThe \"--quiet\" option does not exist.\n")},
	}, PathPrefix{})
	if err == nil {
		t.Fatal("a report with no document must be an error")
	}
	if !strings.Contains(err.Error(), "rector") {
		t.Errorf("error %q does not name the tool that produced it", err)
	}
	if !strings.Contains(err.Error(), "Warning: something printed here") {
		t.Errorf("error %q does not quote what the tool wrote instead", err)
	}
}

func issueLine(t *testing.T, issue map[string]any) float64 {
	t.Helper()

	location, ok := issue["location"].(map[string]any)
	if !ok {
		t.Fatalf("issue has no location: %v", issue)
	}
	lines, ok := location["lines"].(map[string]any)
	if !ok {
		t.Fatalf("location has no lines: %v", location)
	}
	begin, ok := lines["begin"].(float64)
	if !ok {
		t.Fatalf("lines.begin is not a number: %v", lines)
	}
	return begin
}
