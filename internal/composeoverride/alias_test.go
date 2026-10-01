package composeoverride

import (
	"strings"
	"testing"
)

// aliasSrc reaches services only through anchors, aliases and `<<` merge keys. Compose expands
// those itself, so a path written once in an anchor is a path of every service using it.
const aliasSrc = `x-mounts: &mounts
  - ./fixtures:/fixtures
x-one: &one ./single:/single
x-base: &base
  volumes:
    - ./merged:/merged
  env_file: ./merged.env
  build: ./ctx
  image: custom
  labels:
    dev.orobox.url: http://x
x-unused:
  path: ./untouched
services:
  extra:
    volumes: *mounts
  item:
    volumes:
      - *one
      - ./direct:/direct
  application:
    <<: *base
  web:
    volumes: &inline
      - ./inline:/inline
  cron:
    volumes: *inline
`

func TestResolveRewritesThroughAliasesAndMergeKeys(t *testing.T) {
	out, err := Resolve([]byte(aliasSrc), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"/proj/fixtures:/fixtures", "/proj/single:/single", "/proj/merged:/merged",
		"/proj/merged.env", "/proj/ctx", "/proj/direct:/direct", "/proj/inline:/inline",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost %q:\n%s", want, s)
		}
	}
	// Aliases and anchors stay as written: the output is not an expanded copy.
	for _, want := range []string{"&mounts", "*mounts", "&base", "<<: *base", "&inline", "*inline", "&one", "*one"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost anchor syntax %q:\n%s", want, s)
		}
	}
	// A key no service references is not a service path and must stay as written.
	if !strings.Contains(s, "./untouched") {
		t.Errorf("unreferenced extension key rewritten:\n%s", s)
	}
	for _, bad := range []string{"./fixtures", "./single", "./merged", "./ctx", "./inline"} {
		if strings.Contains(s, bad) {
			t.Errorf("relative path %q survived:\n%s", bad, s)
		}
	}
}

func TestAnalyzeThroughAliasesAndMergeKeys(t *testing.T) {
	out, err := Resolve([]byte(aliasSrc), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	a, err := Analyze(out, testCore, func(string) bool { return false })
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	assertStrings(t, missingPaths(a), []string{
		"/proj/fixtures", "/proj/single", "/proj/direct", "/proj/merged", "/proj/inline",
	})
	assertStrings(t, a.CoreImageOverrides, []string{"application"})
	if !a.HasBuild {
		t.Error("HasBuild must see a build inherited through <<")
	}
	if len(a.URLs) != 1 || a.URLs[0] != (ServiceURL{Service: "application", URL: "http://x"}) {
		t.Errorf("URLs = %v", a.URLs)
	}
}

func TestMergeKeyPrecedenceInResolve(t *testing.T) {
	// Only the value Compose actually uses is rewritten: the mapping's own key shadows merged
	// ones, and an earlier merge source shadows a later one. Shadowed values are dead text.
	out, err := Resolve([]byte(`
x-a: &a
  build: ./from-a
  env_file: ./a.env
x-b: &b
  build: ./from-b
  label_file: ./b.labels
services:
  web:
    <<: [*a, *b]
    env_file: ./own.env
`), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s := string(out)
	for _, want := range []string{"/proj/from-a", "/proj/own.env", "/proj/b.labels", "./from-b", "./a.env"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "!!merge") {
		t.Errorf("encoder leaked an explicit merge tag:\n%s", s)
	}
}

func TestMergeKeyLookupPrecedence(t *testing.T) {
	var doc = mustParseMapping(t, "<<: [{k: first}, {k: second, only: b}]\nk: own\nz: own-z\n")
	if got := mapGet(doc, "k"); got == nil || got.Value != "own" {
		t.Errorf("own key must win, got %v", got)
	}
	doc = mustParseMapping(t, "<<: [{k: first}, {k: second, only: b}]\n")
	if got := mapGet(doc, "k"); got == nil || got.Value != "first" {
		t.Errorf("earlier merge source must win, got %v", got)
	}
	if got := mapGet(doc, "only"); got == nil || got.Value != "b" {
		t.Errorf("later merge source must fill gaps, got %v", got)
	}
	if got := mapGet(doc, "absent"); got != nil {
		t.Errorf("absent key = %v", got)
	}
}

func TestResolveRejectsNonMappingRoot(t *testing.T) {
	for _, src := range []string{"- a\n- b\n", "just a scalar\n", "42\n"} {
		out, err := Resolve([]byte(src), testBase, testHome)
		if err == nil || out != nil {
			t.Errorf("Resolve(%q) = %q, %v; want an error", src, out, err)
			continue
		}
		if !strings.Contains(err.Error(), "mapping") {
			t.Errorf("error should name the problem, got %v", err)
		}
	}
}

func TestAnalyzeRejectsNonMappingRoot(t *testing.T) {
	if _, err := Analyze([]byte("- a\n"), testCore, func(string) bool { return true }); err == nil {
		t.Fatal("expected an error for a list root")
	}
}
