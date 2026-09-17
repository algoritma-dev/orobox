package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailuresFromJUnitReadsNestedSuites(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("testdata", "junit-failures.xml"))
	if err != nil {
		t.Fatalf("could not read the fixture: %v", err)
	}

	failures, counts, err := FailuresFromJUnit(doc)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("read %d failures, want 2", len(failures))
	}

	first := failures[0]
	if first.Suite != "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest" {
		t.Errorf("Suite = %q", first.Suite)
	}
	if first.Test != "testTotal" {
		t.Errorf("Test = %q, want %q", first.Test, "testTotal")
	}
	if first.Kind != "failure" {
		t.Errorf("Kind = %q, want %q", first.Kind, "failure")
	}
	if first.Message != "Failed asserting that 2 matches expected 3." {
		t.Errorf("Message = %q", first.Message)
	}
	if first.Line != 88 {
		t.Errorf("Line = %d, want 88", first.Line)
	}

	if failures[1].Kind != "error" {
		t.Errorf("second Kind = %q, want %q", failures[1].Kind, "error")
	}

	want := TestCounts{Passed: 1, Failed: 1, Errored: 1}
	if counts != want {
		t.Errorf("counts = %+v, want %+v", counts, want)
	}
}

func TestFailuresFromJUnitAcceptsABareTestsuiteRoot(t *testing.T) {
	doc := []byte(`<testsuite name="Unit" tests="1" failures="1"><testcase name="t" class="C" line="4"><failure>C::t
boom</failure></testcase></testsuite>`)

	failures, counts, err := FailuresFromJUnit(doc)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed: %v", err)
	}
	if len(failures) != 1 || failures[0].Message != "boom" {
		t.Fatalf("failures = %+v, want one with the message %q", failures, "boom")
	}
	if counts.Failed != 1 || counts.Passed != 0 {
		t.Errorf("counts = %+v, want 1 failed and 0 passed", counts)
	}
}

func TestFailuresFromJUnitOnAnEmptyDocument(t *testing.T) {
	failures, counts, err := FailuresFromJUnit(nil)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed on an empty document: %v", err)
	}
	if len(failures) != 0 || counts != (TestCounts{}) {
		t.Errorf("empty document produced %+v / %+v, want nothing", failures, counts)
	}
}
