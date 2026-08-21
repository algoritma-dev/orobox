package scaffold

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// useTemplates swaps the package's template filesystem for the duration of one test.
func useTemplates(t *testing.T, files fstest.MapFS) {
	t.Helper()
	old := Templates
	Templates = files
	t.Cleanup(func() { Templates = old })
}

type nameData struct{ Name string }

func TestWriteOnceLeavesAnExistingFileAlone(t *testing.T) {
	useTemplates(t, fstest.MapFS{
		"templates/test/stub.tmpl": &fstest.MapFile{Data: []byte("rendered [[.Name]]")},
	})
	root := t.TempDir()
	artifact := Artifact{RelPath: "sub/stub.txt", TemplatePath: "templates/test/stub.tmpl", Ownership: WriteOnce}

	first, err := Write(root, artifact, nameData{"one"})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !first.Written || first.Skipped {
		t.Fatalf("first write = %+v, want Written", first)
	}

	second, err := Write(root, artifact, nameData{"two"})
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if second.Written || !second.Skipped {
		t.Fatalf("second write = %+v, want Skipped", second)
	}

	data, err := os.ReadFile(filepath.Join(root, "sub", "stub.txt"))
	if err != nil {
		t.Fatalf("could not read the written file: %v", err)
	}
	if string(data) != "rendered one" {
		t.Errorf("file = %q, want the first render", data)
	}
}

func TestRewriteRefreshesAnExistingFile(t *testing.T) {
	useTemplates(t, fstest.MapFS{
		"templates/test/stub.tmpl": &fstest.MapFile{Data: []byte("rendered [[.Name]]")},
	})
	root := t.TempDir()
	artifact := Artifact{RelPath: "stub.txt", TemplatePath: "templates/test/stub.tmpl", Ownership: Rewrite}

	if _, err := Write(root, artifact, nameData{"one"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	second, err := Write(root, artifact, nameData{"two"})
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if !second.Written {
		t.Fatalf("second write = %+v, want Written", second)
	}

	data, _ := os.ReadFile(filepath.Join(root, "stub.txt"))
	if string(data) != "rendered two" {
		t.Errorf("file = %q, want the second render", data)
	}
}

func TestWriteAllStopsAtTheFirstFailure(t *testing.T) {
	useTemplates(t, fstest.MapFS{
		"templates/test/ok.tmpl": &fstest.MapFile{Data: []byte("fine")},
	})
	root := t.TempDir()

	results, err := WriteAll(root, []Artifact{
		{RelPath: "ok.txt", TemplatePath: "templates/test/ok.tmpl", Ownership: Rewrite},
		{RelPath: "broken.txt", TemplatePath: "templates/test/missing.tmpl", Ownership: Rewrite},
		{RelPath: "never.txt", TemplatePath: "templates/test/ok.tmpl", Ownership: Rewrite},
	}, nil)

	if err == nil {
		t.Fatal("WriteAll() error = nil, want a failure on the missing template")
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want the one artifact written before the failure", len(results))
	}
	if _, err := os.Stat(filepath.Join(root, "never.txt")); err == nil {
		t.Error("WriteAll() kept going after a failure")
	}
}
