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
