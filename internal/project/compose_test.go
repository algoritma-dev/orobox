package project

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestComposeArgsBaseOnly(t *testing.T) {
	dir := t.TempDir()
	p := Project{Name: "mybundle", InternalDir: dir}

	got := p.ComposeArgs(false)

	want := []string{
		"-p", "mybundle",
		"--project-directory", dir,
		"-f", filepath.Join(dir, "docker-compose.yml"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComposeArgs(false) = %v, want %v", got, want)
	}
}

func TestComposeArgsIncludesSetupFileWhenPresent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.setup.yml"))
	p := Project{Name: "mybundle", InternalDir: dir}

	got := p.ComposeArgs(false)

	want := []string{
		"-p", "mybundle",
		"--project-directory", dir,
		"-f", filepath.Join(dir, "docker-compose.yml"),
		"-f", filepath.Join(dir, "docker-compose.setup.yml"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComposeArgs(false) = %v, want %v", got, want)
	}
}

func TestComposeArgsOmitsTestFileWhenIncludeTestFalse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.test.yml"))
	p := Project{Name: "mybundle", InternalDir: dir}

	got := p.ComposeArgs(false)

	for _, arg := range got {
		if arg == filepath.Join(dir, "docker-compose.test.yml") {
			t.Errorf("ComposeArgs(false) = %v, must not include test file", got)
		}
	}
}

func TestComposeArgsIncludesTestFileWhenIncludeTestTrueAndPresent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.test.yml"))
	p := Project{Name: "mybundle", InternalDir: dir}

	got := p.ComposeArgs(true)

	want := []string{
		"-p", "mybundle",
		"--project-directory", dir,
		"-f", filepath.Join(dir, "docker-compose.yml"),
		"-f", filepath.Join(dir, "docker-compose.test.yml"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComposeArgs(true) = %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}
