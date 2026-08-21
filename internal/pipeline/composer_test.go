package pipeline

import (
	"reflect"
	"testing"
)

func TestLockLayerPathsWithoutRepositories(t *testing.T) {
	got := LockLayerPaths([]byte(`{"name":"acme/shop","require":{"oro/commerce":"6.1.*"}}`))

	want := []string{"composer.json", "composer.lock", "patches", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsIncludesPathRepositories(t *testing.T) {
	manifest := []byte(`{
	  "repositories": [
	    {"type": "path", "url": "packages/*"},
	    {"type": "path", "url": "./lib/theme"},
	    {"type": "vcs", "url": "git@gitlab.com:acme/other.git"},
	    {"type": "path", "url": "/opt/shared"},
	    {"type": "path", "url": "../sibling"}
	  ]
	}`)

	got := LockLayerPaths(manifest)

	// The glob segment is dropped, the relative prefix is cleaned, and anything outside the
	// repository is skipped: Dagger can only mount paths that live in the clone.
	want := []string{"composer.json", "composer.lock", "lib/theme", "packages", "patches", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsAcceptsObjectRepositories(t *testing.T) {
	// Composer accepts repositories as a named object as well as a list.
	manifest := []byte(`{"repositories": {"local": {"type": "path", "url": "packages/theme"}}}`)

	got := LockLayerPaths(manifest)

	want := []string{"composer.json", "composer.lock", "packages/theme", "patches", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsSurvivesBrokenJSON(t *testing.T) {
	// A manifest the pipeline cannot parse must not stop the build: composer itself will
	// report it, with a far better message than anything this parser could produce.
	got := LockLayerPaths([]byte(`{ this is not json`))

	want := []string{"composer.json", "composer.lock", "patches", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsIncludesLocalPatchDirectories(t *testing.T) {
	manifest := []byte(`{
	  "extra": {
	    "patches": {
	      "symfony/http-foundation": {
	        "Patch settare header CSRF se non esiste": "src/Patch/CsrfCookieRequest.patch"
	      },
	      "oro/platform": {
	        "Second patch in the same directory": "src/Patch/Platform.patch"
	      },
	      "acme/root": {"Root patch": "fix.patch"},
	      "acme/remote": {"Downloaded patch": "https://example.test/fix.patch"},
	      "acme/outside": {"Outside the clone": "../shared/fix.patch"}
	    }
	  }
	}`)

	got := LockLayerPaths(manifest)

	// The directory holding the patches is mounted once, a root patch as the file itself, and a
	// patch composer downloads or one living outside the clone contributes nothing.
	want := []string{"composer.json", "composer.lock", "fix.patch", "patches", "src/Patch", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsAcceptsListPatches(t *testing.T) {
	// composer-patches also accepts a package's patches as a list of paths or objects.
	manifest := []byte(`{
	  "extra": {
	    "patches": {
	      "acme/one": ["src/Patch/one.patch", {"url": "build/patches/two.patch"}],
	      "acme/two": [{"path": "build/patches/three.patch"}]
	    }
	  }
	}`)

	got := LockLayerPaths(manifest)

	want := []string{"build/patches", "composer.json", "composer.lock", "patches", "src/Patch", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestLockLayerPathsIncludesPatchesFile(t *testing.T) {
	manifest := []byte(`{"extra": {"patches-file": "build/patches.json"}}`)

	got := LockLayerPaths(manifest)

	want := []string{"build", "composer.json", "composer.lock", "patches", "vendor-bin/qa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockLayerPaths() = %v, want %v", got, want)
	}
}

func TestPatchesFile(t *testing.T) {
	cases := map[string]string{
		`{"extra": {"patches-file": "patches.json"}}`:            "patches.json",
		`{"extra": {"patches-file": "./build/patches.json"}}`:    "build/patches.json",
		`{"extra": {"patches-file": "/etc/patches.json"}}`:       "",
		`{"extra": {"patches-file": "../other/patches.json"}}`:   "",
		`{"extra": {"patches": {"acme/one": {"a": "a.patch"}}}}`: "",
		`{ this is not json`: "",
	}
	for manifest, want := range cases {
		if got := PatchesFile([]byte(manifest)); got != want {
			t.Errorf("PatchesFile(%s) = %q, want %q", manifest, got, want)
		}
	}
}

func TestPatchFilePaths(t *testing.T) {
	// The patches-file carries the same mapping composer.json holds under extra.
	got := PatchFilePaths([]byte(`{"patches": {"acme/one": {"A patch": "src/Patch/one.patch"}}}`))

	want := []string{"src/Patch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PatchFilePaths() = %v, want %v", got, want)
	}

	if got := PatchFilePaths([]byte(`{ this is not json`)); got != nil {
		t.Errorf("PatchFilePaths(broken) = %v, want nil", got)
	}
}
