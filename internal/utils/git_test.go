package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"6.0.1", "6.0.2", -1},
		{"6.0.2", "6.0.1", 1},
		{"6.0.1", "6.0.1", 0},
		{"6.0.1", "6.1.0", -1},
		{"6.0.6", "6.1.0", -1},
		{"6.0", "6.0.1", -1},
		{"6.10.1", "6.2.1", 1},
	}

	for _, tt := range tests {
		got := compareVersions(tt.v1, tt.v2)
		if (got < 0 && tt.want >= 0) || (got > 0 && tt.want <= 0) || (got == 0 && tt.want != 0) {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

// gitRepo builds a repository with the given files staged and returns its root.
func gitRepo(t *testing.T, staged ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.test"}, {"config", "user.name", "Test"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	for _, file := range staged {
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(staged) > 0 {
		cmd := exec.Command("git", append([]string{"-C", root, "add", "--"}, staged...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v: %s", err, out)
		}
	}
	return root
}

func TestStagedFiles(t *testing.T) {
	root := gitRepo(t, "src/Entity.php", "src/Resources/views/list.html.twig", "README.md")

	files, err := StagedFiles(root)
	if err != nil {
		t.Fatalf("StagedFiles: %v", err)
	}

	want := map[string]bool{"src/Entity.php": true, "src/Resources/views/list.html.twig": true, "README.md": true}
	if len(files) != len(want) {
		t.Fatalf("StagedFiles returned %v, want %d entries", files, len(want))
	}
	for _, file := range files {
		if !want[file] {
			t.Errorf("StagedFiles returned unexpected %q", file)
		}
	}
}

// TestStagedFilesRelativeToDir covers the bundle layout, where .orobox.yaml sits below the
// repository root: the paths have to be relative to that directory, and the staged files outside it
// have to be gone rather than reported with a path no tool can open.
func TestStagedFilesRelativeToDir(t *testing.T) {
	root := gitRepo(t, "bundle/src/Entity.php", "outside/Other.php")

	files, err := StagedFiles(filepath.Join(root, "bundle"))
	if err != nil {
		t.Fatalf("StagedFiles: %v", err)
	}

	if len(files) != 1 || files[0] != "src/Entity.php" {
		t.Errorf("StagedFiles = %v, want [src/Entity.php]", files)
	}
}

// TestStagedFilesIgnoresDeletions: a file the commit removes cannot be opened by any tool.
func TestStagedFilesIgnoresDeletions(t *testing.T) {
	root := gitRepo(t, "src/Gone.php", "src/Kept.php")
	for _, args := range [][]string{{"commit", "-m", "seed"}, {"rm", "src/Gone.php"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	files, err := StagedFiles(root)
	if err != nil {
		t.Fatalf("StagedFiles: %v", err)
	}
	for _, file := range files {
		if file == "src/Gone.php" {
			t.Errorf("StagedFiles reported the deleted src/Gone.php: %v", files)
		}
	}
}

func TestStagedFilesOutsideRepository(t *testing.T) {
	if _, err := StagedFiles(t.TempDir()); err == nil {
		t.Error("StagedFiles reported no error outside a git repository")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/var/www/oro/src/Entity.php", `'/var/www/oro/src/Entity.php'`},
		{"/var/www/My Bundle/a.php", `'/var/www/My Bundle/a.php'`},
		{"/var/www/it's/a.php", `'/var/www/it'\''s/a.php'`},
	}
	for _, tt := range tests {
		if got := ShellQuote(tt.in); got != tt.want {
			t.Errorf("ShellQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
