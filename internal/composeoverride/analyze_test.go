package composeoverride

import (
	"strings"
	"testing"
)

var testCore = []string{"application", "web", "database"}

func analyze(t *testing.T, src string, exists func(string) bool) Analysis {
	t.Helper()
	if exists == nil {
		exists = func(string) bool { return true }
	}
	a, err := Analyze([]byte(src), testCore, exists)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return a
}

func TestAnalyzeMissingPaths(t *testing.T) {
	src := `
services:
  application:
    volumes:
      - /host/present:/a
      - /host/gone:/b:ro
      - named:/c
      - ${VAR}/x:/d
      - {type: bind, source: /host/long-gone, target: /e}
      - {type: bind, source: /host/present, target: /f}
      - {type: volume, source: /not/checked, target: /g}
  web:
    volumes:
      - /host/gone:/again
`
	a := analyze(t, src, func(p string) bool { return strings.HasPrefix(p, "/host/present") })
	assertStrings(t, missingPaths(a), []string{"/host/gone", "/host/long-gone"})
}

func TestAnalyzeNothingMissing(t *testing.T) {
	a := analyze(t, "services:\n  w:\n    volumes:\n      - /p:/q\n", nil)
	if len(a.MissingPaths) != 0 {
		t.Errorf("MissingPaths = %v", a.MissingPaths)
	}
}

func TestAnalyzeCoreImageOverrides(t *testing.T) {
	a := analyze(t, `
services:
  application:
    image: x
  web:
    environment: {A: b}
  extra:
    image: y
  database:
    image: z
`, nil)
	assertStrings(t, a.CoreImageOverrides, []string{"application", "database"})
}

func TestAnalyzeHasBuild(t *testing.T) {
	if a := analyze(t, "services:\n  extra:\n    image: y\n", nil); a.HasBuild {
		t.Error("HasBuild true without a build key")
	}
	if a := analyze(t, "services:\n  extra:\n    build: ./x\n", nil); !a.HasBuild {
		t.Error("HasBuild false with a build key")
	}
	if a := analyze(t, "services:\n  extra:\n    build: {context: .}\n", nil); !a.HasBuild {
		t.Error("HasBuild false with a mapping build")
	}
}

func TestAnalyzeURLs(t *testing.T) {
	a := analyze(t, `
services:
  mail:
    labels:
      dev.orobox.url: http://localhost:8025
      other: x
  adminer:
    labels:
      - other=y
      - dev.orobox.url=http://localhost:8081/?a=b
  plain:
    labels: {foo: bar}
`, nil)
	want := []ServiceURL{
		{Service: "adminer", URL: "http://localhost:8081/?a=b"},
		{Service: "mail", URL: "http://localhost:8025"},
	}
	if len(a.URLs) != len(want) {
		t.Fatalf("URLs = %v, want %v", a.URLs, want)
	}
	for i := range want {
		if a.URLs[i] != want[i] {
			t.Errorf("URLs[%d] = %v, want %v", i, a.URLs[i], want[i])
		}
	}
}

func TestAnalyzeEmptyDocument(t *testing.T) {
	a, err := Analyze(nil, testCore, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(a.MissingPaths) != 0 || len(a.CoreImageOverrides) != 0 || a.HasBuild || len(a.URLs) != 0 {
		t.Errorf("expected zero Analysis, got %+v", a)
	}
}

func TestAnalyzeInvalidYAML(t *testing.T) {
	if _, err := Analyze([]byte("services: [unclosed"), testCore, func(string) bool { return true }); err == nil {
		t.Fatal("expected an error for invalid YAML")
	}
}

func TestAnalyzeMissingPathSyntaxAndSkips(t *testing.T) {
	src := `
services:
  a:
    volumes:
      - /gone/short:/a
      - {type: bind, source: /gone/long, target: /b}
      - {type: bind, source: /gone/created, target: /c, bind: {create_host_path: true}}
      - /gone/${VAR}/x:/d
      - {type: bind, source: "/gone/$${x}", target: /e}
      - /gone/$$lit:/f
`
	var checked []string
	a := analyze(t, src, func(p string) bool { checked = append(checked, p); return false })
	want := []MissingPath{
		{Path: "/gone/short"},
		{Path: "/gone/long", LongSyntax: true},
		{Path: "/gone/${x}", LongSyntax: true},
		{Path: "/gone/$lit"},
	}
	if len(a.MissingPaths) != len(want) {
		t.Fatalf("MissingPaths = %+v, want %+v (checked %v)", a.MissingPaths, want, checked)
	}
	for i := range want {
		if a.MissingPaths[i] != want[i] {
			t.Errorf("MissingPaths[%d] = %+v, want %+v", i, a.MissingPaths[i], want[i])
		}
	}
}

func TestAnalyzeIgnoresResetImageAndBuild(t *testing.T) {
	a := analyze(t, `
services:
  application:
    image: !reset null
  web:
    image: ~
  extra:
    build: !reset
  other:
    build: null
`, nil)
	if len(a.CoreImageOverrides) != 0 {
		t.Errorf("an unset image is not an override, got %v", a.CoreImageOverrides)
	}
	if a.HasBuild {
		t.Error("an unset build must not count as a build")
	}
}

// `up` does not start a service that declares profiles, so its URL would point at nothing.
func TestAnalyzeSkipsURLsOfProfiledServices(t *testing.T) {
	a := analyze(t, `
services:
  tools:
    profiles: [tools]
    labels:
      dev.orobox.url: http://localhost:9000
  web:
    labels:
      dev.orobox.url: http://localhost:8080
`, nil)
	if len(a.URLs) != 1 || a.URLs[0].Service != "web" {
		t.Errorf("URLs = %v, want only web", a.URLs)
	}
}

func TestAnalyzeMultiDocument(t *testing.T) {
	a := analyze(t, "services:\n  application:\n    image: x\n---\nservices:\n  extra:\n    build: ./x\n    volumes:\n      - /gone:/g\n", func(string) bool { return false })
	assertStrings(t, a.CoreImageOverrides, []string{"application"})
	if !a.HasBuild || len(a.MissingPaths) != 1 {
		t.Errorf("every document must be analyzed, got %+v", a)
	}
}

// missingPaths returns the paths of a.MissingPaths, in order.
func missingPaths(a Analysis) []string {
	out := make([]string, len(a.MissingPaths))
	for i, m := range a.MissingPaths {
		out[i] = m.Path
	}
	return out
}

func TestAnalyzeReportsProfiledServices(t *testing.T) {
	a, err := Analyze([]byte("services:\n  tools:\n    image: x\n    profiles: [tools]\n  web:\n    image: y\n"), nil, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Profiled) != 1 || a.Profiled[0] != "tools" {
		t.Errorf("Profiled = %v, want [tools]", a.Profiled)
	}
}

// A later `profiles: !reset []` (or an empty list) clears the profiles an earlier document gave.
func TestAnalyzeProfilesFollowTheLastDocument(t *testing.T) {
	a, err := Analyze([]byte("services:\n  tool:\n    image: x\n    profiles: [tools]\n    labels:\n      dev.orobox.url: http://localhost:1\n---\nservices:\n  tool:\n    profiles: !reset []\n"), nil, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Profiled) != 0 || len(a.URLs) != 1 {
		t.Errorf("Profiled = %v, URLs = %v; want no profiles and the URL listed", a.Profiled, a.URLs)
	}
	if len(a.Unprofiled) != 1 || a.Unprofiled[0] != "tool" {
		t.Errorf("Unprofiled = %v, want [tool]", a.Unprofiled)
	}
}
