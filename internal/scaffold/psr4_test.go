package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeComposerJSON(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvePsr4Dir(t *testing.T) {
	tests := []struct {
		name      string
		composer  string
		namespace string
		wantDir   string
		wantRel   string
		wantErr   error
	}{
		{
			// The rule every OroCommerce application skeleton ships.
			name:      "oro application root map",
			composer:  `{"autoload":{"psr-4":{"":"src/"}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   "src",
			wantRel:   filepath.Join("src", "Acme", "Bundle", "FooBundle"),
		},
		{
			name:      "longest prefix wins",
			composer:  `{"autoload":{"psr-4":{"":"src/","Acme\\Bundle\\":"lib/"}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   "lib",
			wantRel:   filepath.Join("lib", "FooBundle"),
		},
		{
			// A prefix that does not cover the namespace is skipped even when it is longer.
			name:      "non-matching prefix is skipped",
			composer:  `{"autoload":{"psr-4":{"":"src/","Other\\Long\\Prefix\\":"lib/"}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   "src",
			wantRel:   filepath.Join("src", "Acme", "Bundle", "FooBundle"),
		},
		{
			// A prefix must match on a segment boundary: Acme\ does not cover AcmeOther\.
			name:      "partial segment is not a match",
			composer:  `{"autoload":{"psr-4":{"Acme\\":"lib/"}}}`,
			namespace: `AcmeOther\Bundle\FooBundle`,
			wantErr:   ErrNoPsr4Root,
		},
		{
			// Composer allows a list of roots; the first is where a new class is found.
			name:      "list value takes the first directory",
			composer:  `{"autoload":{"psr-4":{"":["src/","extra/"]}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   "src",
			wantRel:   filepath.Join("src", "Acme", "Bundle", "FooBundle"),
		},
		{
			// A prefix mapped to the package root cannot host a namespace subtree without
			// colliding with the package's own files, so the next prefix is tried.
			name:      "package-root map falls through to the next prefix",
			composer:  `{"autoload":{"psr-4":{"Acme\\Bundle\\FooBundle\\":"","":"src/"}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   "src",
			wantRel:   filepath.Join("src", "Acme", "Bundle", "FooBundle"),
		},
		{
			name:      "namespace equal to the prefix lands in the root itself",
			composer:  `{"autoload":{"psr-4":{"Acme\\Bundle\\FooBundle\\":"lib/foo/"}}}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantDir:   filepath.Join("lib", "foo"),
			wantRel:   filepath.Join("lib", "foo"),
		},
		{
			name:      "no psr-4 section",
			composer:  `{"name":"acme/thing"}`,
			namespace: `Acme\Bundle\FooBundle`,
			wantErr:   ErrNoPsr4Root,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeComposerJSON(t, t.TempDir(), tt.composer)

			got, err := ResolvePsr4Dir(root, tt.namespace)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", got.Dir, tt.wantDir)
			}
			if got.RelPath != tt.wantRel {
				t.Errorf("relPath = %q, want %q", got.RelPath, tt.wantRel)
			}
		})
	}
}

// A directory with no composer.json is not an error the user has to fix: it is the standalone
// case, so the sentinel has to be distinguishable.
func TestResolvePsr4DirWithoutComposerJson(t *testing.T) {
	if _, err := ResolvePsr4Dir(t.TempDir(), `Acme\Bundle\FooBundle`); !errors.Is(err, ErrNoPsr4Root) {
		t.Fatalf("error = %v, want ErrNoPsr4Root", err)
	}
}

// A composer.json that exists but cannot be parsed is a real problem, and must not be
// mistaken for "this is not a PHP project" — that would silently scaffold a standalone bundle
// inside a project.
func TestResolvePsr4DirReportsUnparseableComposerJson(t *testing.T) {
	root := writeComposerJSON(t, t.TempDir(), `{"autoload":`)

	_, err := ResolvePsr4Dir(root, `Acme\Bundle\FooBundle`)
	if err == nil {
		t.Fatal("expected an error for a broken composer.json")
	}
	if errors.Is(err, ErrNoPsr4Root) {
		t.Fatal("a broken composer.json must not be reported as a missing PSR-4 root")
	}
}

func TestResolvePsr4DirRejectsAnUnusablePsr4Value(t *testing.T) {
	root := writeComposerJSON(t, t.TempDir(), `{"autoload":{"psr-4":{"":42}}}`)

	_, err := ResolvePsr4Dir(root, `Acme\Bundle\FooBundle`)
	if err == nil || errors.Is(err, ErrNoPsr4Root) {
		t.Fatalf("error = %v, want a parse failure naming the bad entry", err)
	}
}
