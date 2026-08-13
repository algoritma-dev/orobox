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
