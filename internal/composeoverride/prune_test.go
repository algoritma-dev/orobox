package composeoverride

import (
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

func prune(t *testing.T, src string, known ...string) (map[string]any, []string, string) {
	t.Helper()
	out, dropped, err := Prune([]byte(src), known)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	var doc map[string]any
	if err := yamlv3.Unmarshal(out, &doc); err != nil {
		t.Fatalf("pruned output is not valid YAML: %v\n%s", err, out)
	}
	return doc, dropped, string(out)
}

// A service the generated stack does not define, and that cannot stand on its own (no image,
// build or extends), makes compose reject the whole project. It is dropped; everything that
// either the stack defines or that brings its own image stays.
func TestPruneDropsServicesOutsideTheStack(t *testing.T) {
	doc, dropped, out := prune(t, `
x-img: &img
  image: busybox
services:
  application:
    volumes:
      - /a:/a
  db-test:
    environment: {A: b}
  minio:
    image: minio/minio
  docs:
    build: /proj/docs
  child:
    extends: {file: /proj/base.yaml, service: base}
  merged:
    <<: *img
  reset:
    image: !reset null
`, "application", "web")
	assertStrings(t, dropped, []string{"db-test", "reset"})
	services := doc["services"].(map[string]any)
	for _, keep := range []string{"application", "minio", "docs", "child", "merged"} {
		if _, ok := services[keep]; !ok {
			t.Errorf("%s must be kept:\n%s", keep, out)
		}
	}
	for _, gone := range dropped {
		if _, ok := services[gone]; ok {
			t.Errorf("%s must be dropped:\n%s", gone, out)
		}
	}
}

// An anchor defined on a dropped service may still be used by a kept one; the use must keep
// working rather than point at an anchor that is no longer in the file.
func TestPruneKeepsAnchorsOfDroppedServicesUsable(t *testing.T) {
	doc, dropped, out := prune(t, `
services:
  db-test: &common
    environment: &env
      A: b
  extra:
    <<: *common
    image: busybox
    labels: *env
`, "application")
	assertStrings(t, dropped, []string{"db-test"})
	extra := service(t, doc, "extra")
	if extra["environment"].(map[string]any)["A"] != "b" || extra["labels"].(map[string]any)["A"] != "b" {
		t.Errorf("the merged content must survive the drop:\n%s", out)
	}
}

// Compose merges the documents of a file in order, so an image given in one document makes
// the service standalone in all of them.
func TestPruneMultiDocument(t *testing.T) {
	_, dropped, out := prune(t, "services:\n  minio:\n    image: minio\n  ghost:\n    environment: {A: b}\n---\nservices:\n  minio:\n    environment: {B: c}\n  ghost:\n    labels: {a: b}\n")
	assertStrings(t, dropped, []string{"ghost"})
	if strings.Count(out, "minio") < 2 || strings.Contains(out, "ghost") {
		t.Errorf("minio must stay in both documents and ghost go from both:\n%s", out)
	}
}

func TestPruneNothingToDo(t *testing.T) {
	out, dropped, err := Prune(nil, []string{"application"})
	if err != nil || out != nil || dropped != nil {
		t.Errorf("Prune(nil) = %q, %v, %v; want nil, nil, nil", out, dropped, err)
	}
	src := "services:\n  application:\n    environment: {A: b}\n"
	out, dropped, err = Prune([]byte(src), []string{"application"})
	if err != nil || len(dropped) != 0 || string(out) != src {
		t.Errorf("an override with nothing to drop must come back unchanged, got %q, %v, %v", out, dropped, err)
	}
}

func TestServiceNames(t *testing.T) {
	names, err := ServiceNames([]byte("services:\n  web:\n    image: x\n  application:\n    image: y\n---\nservices:\n  db-test:\n    image: z\n"))
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, names, []string{"application", "db-test", "web"})
}

// A service that reaches `services:` only through a merge key cannot be taken out of the file;
// reporting it as dropped while leaving it in would let compose fail anyway.
func TestPruneRefusesAServiceItCannotRemove(t *testing.T) {
	src := "x-extra: &extra\n  ghost:\n    environment:\n      A: b\nservices:\n  <<: *extra\n  application:\n    environment:\n      B: c\n"
	_, _, err := Prune([]byte(src), []string{"application"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("want an error naming ghost, got %v", err)
	}
}

// An entry that also arrives through a merge key cannot be fully removed, so pruning refuses
// rather than leave compose a half-removed service it rejects anyway.
func TestPruneRefusesAServiceAlsoMergedIn(t *testing.T) {
	src := "x-extra: &extra\n  db-test:\n    environment:\n      A: b\nservices:\n  <<: *extra\n  db-test:\n    environment:\n      C: d\n"
	if _, _, err := Prune([]byte(src), []string{"application"}); err == nil || !strings.Contains(err.Error(), "db-test") {
		t.Errorf("want an error naming db-test, got %v", err)
	}
}
