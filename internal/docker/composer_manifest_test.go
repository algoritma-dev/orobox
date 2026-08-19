package docker

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeManifest writes composer.json into a fresh temp dir and returns the dir.
func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write composer.json: %v", err)
	}
	return dir
}

func TestManifestRepoURLs_ListForm(t *testing.T) {
	dir := writeManifest(t, `{
		"name": "acme/app",
		"repositories": [
			{"type": "vcs", "url": "git@github.com:acme/private.git"},
			{"type": "composer", "url": "https://repo.packagist.com/acme/"}
		]
	}`)

	got := manifestRepoURLs(dir)
	sort.Strings(got)
	want := []string{"git@github.com:acme/private.git", "https://repo.packagist.com/acme/"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("manifestRepoURLs = %v, want %v", got, want)
	}
}

// Composer also accepts repositories as a map of name to definition.
func TestManifestRepoURLs_NamedForm(t *testing.T) {
	dir := writeManifest(t, `{
		"repositories": {
			"private": {"type": "vcs", "url": "ssh://git@gitlab.example.com/acme/private.git"}
		}
	}`)

	got := manifestRepoURLs(dir)
	if len(got) != 1 || got[0] != "ssh://git@gitlab.example.com/acme/private.git" {
		t.Errorf("manifestRepoURLs = %v, want the single ssh:// URL", got)
	}
}

func TestManifestRepoURLs_EmptyCases(t *testing.T) {
	cases := map[string]string{
		"no repositories key": `{"name": "acme/app"}`,
		"empty list":          `{"repositories": []}`,
		"malformed json":      `{"repositories": [`,
		"entries without url": `{"repositories": [{"type": "path"}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if got := manifestRepoURLs(writeManifest(t, content)); len(got) != 0 {
				t.Errorf("manifestRepoURLs = %v, want none", got)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		if got := manifestRepoURLs(t.TempDir()); got != nil {
			t.Errorf("manifestRepoURLs = %v, want nil", got)
		}
	})

	t.Run("empty dir argument", func(t *testing.T) {
		if got := manifestRepoURLs(""); got != nil {
			t.Errorf("manifestRepoURLs = %v, want nil", got)
		}
	})
}
