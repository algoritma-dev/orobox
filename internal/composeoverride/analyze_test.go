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
	assertStrings(t, a.MissingPaths, []string{"/host/gone", "/host/long-gone"})
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
