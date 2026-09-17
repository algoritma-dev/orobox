package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFindingsCarriesToolAttributionAndSorts(t *testing.T) {
	reports := []ToolReport{
		{Tool: "eslint", Data: []byte(`[{"check_name":"semi","description":"Missing semicolon.","severity":"major","location":{"path":"/var/www/oro/src/b.js","lines":{"begin":11}}}]`)},
		{Tool: "phpstan", Data: []byte(`[{"description":"Access to undefined property.","severity":"blocker","location":{"path":"/var/www/oro/src/a.php","lines":{"begin":42}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}

	want := []Finding{
		{Tool: "phpstan", Path: "src/a.php", Line: 42, Severity: "blocker", Message: "Access to undefined property."},
		{Tool: "eslint", Path: "src/b.js", Line: 11, Check: "semi", Severity: "major", Message: "Missing semicolon."},
	}
	if len(got) != len(want) {
		t.Fatalf("Findings() returned %d findings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindingsCollapsesMultiLineMessages(t *testing.T) {
	reports := []ToolReport{
		{Tool: "phpstan", Data: []byte(`[{"description":"Parameter #1 $x\n  expects int,\n  string given.","location":{"path":"a.php","lines":{"begin":1}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}
	want := "Parameter #1 $x expects int, string given."
	if got[0].Message != want {
		t.Errorf("Message = %q, want %q", got[0].Message, want)
	}
}

func TestFindingsDefaultsSeverityWhenTheToolOmitsIt(t *testing.T) {
	reports := []ToolReport{
		{Tool: "twig-cs-fixer", Data: []byte(`[{"description":"d","location":{"path":"a.twig","lines":{"begin":2}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}
	if got[0].Severity != "info" {
		t.Errorf("Severity = %q, want %q", got[0].Severity, "info")
	}
}

func TestFindingsReadsEveryCapturedFixture(t *testing.T) {
	for _, tool := range []string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint"} {
		t.Run(tool, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tool+".json"))
			if err != nil {
				t.Fatalf("could not read the fixture: %v", err)
			}

			got, err := Findings([]ToolReport{{Tool: tool, Data: data}}, PathPrefix{ContainerRoot: "/var/www/oro"})
			if err != nil {
				t.Fatalf("Findings() failed on the real %s output: %v", tool, err)
			}
			if len(got) == 0 {
				t.Fatalf("Findings() read no findings from the real %s output", tool)
			}
			for i, f := range got {
				if f.Tool != tool {
					t.Errorf("finding %d has tool %q, want %q", i, f.Tool, tool)
				}
				if f.Path == "" {
					t.Errorf("finding %d has no path", i)
				}
				if f.Message == "" {
					t.Errorf("finding %d has no message", i)
				}
			}
		})
	}
}

func TestFindingsStripsPHPStanBugReportBoilerplate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "internal error",
			in:   "Internal error: Call to a member function set() on int\nRun PHPStan with -v option and post the stack trace to:\nhttps://github.com/phpstan/phpstan/issues/new?template=Bug_report.yaml",
			want: "Internal error: Call to a member function set() on int",
		},
		{
			name: "no trailing url",
			in:   "Internal error: boom\nRun PHPStan with -v option and post the stack trace to:",
			want: "Internal error: boom",
		},
		{
			name: "ordinary message is untouched",
			in:   "Access to an undefined property App\\Entity\\Order::$foo.",
			want: "Access to an undefined property App\\Entity\\Order::$foo.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issue := map[string]any{"description": tc.in, "location": map[string]any{"path": "a.php"}}
			data, err := json.Marshal([]map[string]any{issue})
			if err != nil {
				t.Fatal(err)
			}

			got, err := Findings([]ToolReport{{Tool: "phpstan", Data: data}}, PathPrefix{})
			if err != nil {
				t.Fatalf("Findings() failed: %v", err)
			}
			if got[0].Message != tc.want {
				t.Errorf("Message = %q, want %q", got[0].Message, tc.want)
			}
		})
	}
}
