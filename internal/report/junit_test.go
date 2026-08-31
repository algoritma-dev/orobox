package report

import (
	"encoding/xml"
	"strings"
	"testing"
)

const unitSuiteXML = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="unit" tests="2" failures="1">
    <testcase name="testAdds" class="Acme\CalculatorTest" time="0.01"/>
    <testcase name="testFails" class="Acme\CalculatorTest" time="0.02">
      <failure type="PHPUnit\Framework\ExpectationFailedException">expected 3</failure>
    </testcase>
  </testsuite>
</testsuites>`

const functionalSuiteXML = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="functional" tests="1" failures="0">
    <testcase name="testLoads" class="Acme\ControllerTest" time="1.5"/>
  </testsuite>
</testsuites>`

func TestMergeJUnitKeepsEverySuite(t *testing.T) {
	merged, err := MergeJUnit([][]byte{[]byte(unitSuiteXML), []byte(functionalSuiteXML)})
	if err != nil {
		t.Fatalf("MergeJUnit returned %v", err)
	}

	var parsed struct {
		XMLName xml.Name `xml:"testsuites"`
		Suites  []struct {
			Name  string `xml:"name,attr"`
			Tests string `xml:"tests,attr"`
			Cases []struct {
				Name string `xml:"name,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("merged document is not valid XML: %v", err)
	}

	if len(parsed.Suites) != 2 {
		t.Fatalf("merged %d suites, want 2", len(parsed.Suites))
	}
	if parsed.Suites[0].Name != "unit" || parsed.Suites[1].Name != "functional" {
		t.Errorf("suite names = %q, %q; want unit, functional", parsed.Suites[0].Name, parsed.Suites[1].Name)
	}
	if parsed.Suites[0].Tests != "2" {
		t.Errorf("the unit suite lost its attributes: tests = %q", parsed.Suites[0].Tests)
	}
	if len(parsed.Suites[0].Cases) != 2 {
		t.Errorf("the unit suite has %d cases, want 2", len(parsed.Suites[0].Cases))
	}
	if !strings.Contains(string(merged), "expected 3") {
		t.Error("the failure message was dropped; nested elements must survive the merge")
	}
}

func TestMergeJUnitAcceptsABareTestsuiteRoot(t *testing.T) {
	bare := `<testsuite name="unit" tests="1"><testcase name="testAdds"/></testsuite>`

	merged, err := MergeJUnit([][]byte{[]byte(bare)})
	if err != nil {
		t.Fatalf("MergeJUnit returned %v", err)
	}
	if !strings.HasPrefix(string(merged), "<?xml") {
		t.Errorf("merged document has no XML declaration: %s", merged)
	}
	if !strings.Contains(string(merged), `<testsuites>`) {
		t.Errorf("a bare testsuite must be wrapped in testsuites: %s", merged)
	}
}

func TestMergeJUnitEmptyInputIsAValidDocument(t *testing.T) {
	merged, err := MergeJUnit(nil)
	if err != nil {
		t.Fatalf("MergeJUnit returned %v", err)
	}

	var parsed struct {
		XMLName xml.Name `xml:"testsuites"`
	}
	if err := xml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("the empty document is not valid XML: %v", err)
	}
}

func TestMergeJUnitRejectsGarbage(t *testing.T) {
	_, err := MergeJUnit([][]byte{[]byte("PHP Fatal error: allowed memory size exhausted")})
	if err == nil {
		t.Fatal("a non-XML document must be an error")
	}
}
