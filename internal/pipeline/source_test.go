package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestHostExcludesFallsBackWithoutGit(t *testing.T) {
	dir := t.TempDir()

	excludes := HostExcludes(dir)

	for _, want := range []string{"vendor", "node_modules", "var", ".git"} {
		if !slices.Contains(excludes, want) {
			t.Errorf("the fallback list is missing %q: %v", want, excludes)
		}
	}
}

func TestHostExcludesReadsGitIgnoredPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(".gitignore", "vendor/\npublic/build/\n")
	write("vendor/autoload.php", "<?php")
	write("public/build/app.js", "//")
	write("src/Kernel.php", "<?php")

	excludes := HostExcludes(dir)

	if !slices.Contains(excludes, "vendor/") {
		t.Errorf("an ignored directory is not excluded: %v", excludes)
	}
	if !slices.Contains(excludes, "public/build/") {
		t.Errorf("an ignored build directory is not excluded: %v", excludes)
	}
	if slices.Contains(excludes, "src/") || slices.Contains(excludes, "src/Kernel.php") {
		t.Errorf("a tracked path must not be excluded: %v", excludes)
	}
	if !slices.Contains(excludes, ".git") {
		t.Errorf(".git must always be excluded, git never lists it as ignored: %v", excludes)
	}
}

// The `vendor-bin/qa` contenthash failure in CI reported a path buildkit could not stat, and the
// only thing the log said about the exclude list was its length — 140 for the run that worked,
// 142 for the one that failed. Which patterns those were, and whether the named path was even
// among them, was unrecoverable after the fact.
//
// MissingExcludes names the entries that do not exist on disk. That is not an error on its own:
// the list is a point-in-time snapshot from a git subprocess, and orobox runs containers that
// write into a project checkout between that call and the tree being read. It is logged so the
// next occurrence identifies the pattern instead of requiring this reconstruction.
func TestMissingExcludesNamesWhatIsNotOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := MissingExcludes(dir, []string{"vendor", "kept.txt", "vendor-bin/qa", "var/cache"})

	want := map[string]bool{"vendor-bin/qa": true, "var/cache": true}
	if len(got) != len(want) {
		t.Fatalf("MissingExcludes() = %v, want the two absent paths", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("MissingExcludes() reported %q, which exists", p)
		}
	}
}

func TestMissingExcludesIsEmptyWhenEverythingExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "var", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := MissingExcludes(dir, []string{"var/cache"}); len(got) != 0 {
		t.Errorf("MissingExcludes() = %v, want empty", got)
	}
}
