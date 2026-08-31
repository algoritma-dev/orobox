package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	yamlv3 "gopkg.in/yaml.v3"
)

func testTemplates() fstest.MapFS {
	return fstest.MapFS{
		"templates/bundle/bundle.php.tmpl":        {Data: []byte("<?php\nnamespace [[.Namespace]];\nclass [[.ClassName]] extends Bundle {}\n")},
		"templates/bundle/Extension.php.tmpl":     {Data: []byte("<?php\nnamespace [[.Namespace]]\\DependencyInjection;\nclass [[.Prefix]]Extension {}\n")},
		"templates/bundle/Configuration.php.tmpl": {Data: []byte("<?php\nnamespace [[.Namespace]]\\DependencyInjection;\n// tree [[.Alias]]\n")},
		"templates/bundle/services.yml.tmpl":      {Data: []byte("services:\n")},
		"templates/bundle/bundles.yml.tmpl":       {Data: []byte("bundles:\n    - { name: [[.Namespace]]\\[[.ClassName]], priority: 30 }\n")},
		"templates/bundle/composer.json.tmpl":     {Data: []byte(`{"name":"[[.PackageName]]","type":"symfony-bundle","autoload":{"psr-4":{"[[esc .Namespace]]\\":""}}}`)},
		"templates/bundle/gitignore.tmpl":         {Data: []byte("/vendor/\n/vendor-oro/\n")},
	}
}

func TestParseBundleArg(t *testing.T) {
	tests := []struct {
		name        string
		arg         string
		classFlag   string
		packageFlag string
		want        BundleOptions
		wantErr     bool
	}{
		{
			// The Oro layout: the vendor and the bundle segment make the class name, exactly
			// as Oro\Bundle\UserBundle makes OroUserBundle.
			name: "oro-style namespace",
			arg:  `Acme\Bundle\FooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\Bundle\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			name: "two-segment namespace",
			arg:  `Acme\FooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			// The class form is told apart by the segment before the last one being a
			// *Bundle namespace of its own.
			name: "fully qualified class",
			arg:  `Acme\Bundle\FooBundle\AcmeFooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\Bundle\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			// A bundle segment that already starts with the vendor is not doubled into
			// AcmeAcmeFooBundle.
			name: "bundle segment already carries the vendor",
			arg:  `Acme\Bundle\AcmeFooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\Bundle\AcmeFooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/acme-foo-bundle",
			},
		},
		{
			name: "single segment",
			arg:  "FooBundle",
			want: BundleOptions{
				ClassName:   "FooBundle",
				Namespace:   "FooBundle",
				Prefix:      "Foo",
				Alias:       "foo",
				PackageName: "orobox/foo-bundle",
			},
		},
		{
			name:      "class flag overrides the derived class",
			arg:       `Acme\Bundle\FooBundle`,
			classFlag: "CustomBundle",
			want: BundleOptions{
				ClassName:   "CustomBundle",
				Namespace:   `Acme\Bundle\FooBundle`,
				Prefix:      "Custom",
				Alias:       "custom",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			name:        "package flag overrides the derived package",
			arg:         `Acme\Bundle\FooBundle`,
			packageFlag: "custom/pkg",
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\Bundle\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "custom/pkg",
			},
		},
		{
			// A leading backslash is how a PHP developer writes a root namespace; it is not
			// part of the namespace itself.
			name: "leading backslash is trimmed",
			arg:  `\Acme\Bundle\FooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\Bundle\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{name: "empty arg errors", arg: "", wantErr: true},
		{name: "empty segment errors", arg: `Acme\\FooBundle`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBundleArg(tt.arg, tt.classFlag, tt.packageFlag)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseBundleArg(%q, %q, %q):\n got  %+v\n want %+v", tt.arg, tt.classFlag, tt.packageFlag, got, tt.want)
			}
		})
	}
}

func TestBundleWritesStandaloneSkeleton(t *testing.T) {
	useTemplates(t, testTemplates())

	dest := filepath.Join(t.TempDir(), "AcmeFooBundle")
	opts := BundleOptions{
		ClassName:   "AcmeFooBundle",
		Namespace:   `Acme\Bundle\FooBundle`,
		Prefix:      "AcmeFoo",
		Alias:       "acme_foo",
		PackageName: "acme/foo-bundle",
		Standalone:  true,
	}

	if err := Bundle(dest, opts); err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	wantFiles := []string{
		"AcmeFooBundle.php",
		"DependencyInjection/AcmeFooExtension.php",
		"DependencyInjection/Configuration.php",
		"Resources/config/services.yml",
		"Resources/config/oro/bundles.yml",
		"composer.json",
		".gitignore",
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(f))); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}

	classContent, _ := os.ReadFile(filepath.Join(dest, "AcmeFooBundle.php"))
	if !strings.Contains(string(classContent), `namespace Acme\Bundle\FooBundle;`) ||
		!strings.Contains(string(classContent), "class AcmeFooBundle") {
		t.Errorf("bundle class content wrong:\n%s", classContent)
	}

	composer, _ := os.ReadFile(filepath.Join(dest, "composer.json"))
	var cj map[string]any
	if err := json.Unmarshal(composer, &cj); err != nil {
		t.Errorf("composer.json is not valid JSON: %v\n%s", err, composer)
	}
	if cj["name"] != "acme/foo-bundle" {
		t.Errorf("composer name = %v, want acme/foo-bundle", cj["name"])
	}

	bundles, _ := os.ReadFile(filepath.Join(dest, filepath.FromSlash("Resources/config/oro/bundles.yml")))
	var by map[string]any
	if err := yamlv3.Unmarshal(bundles, &by); err != nil {
		t.Errorf("bundles.yml is not valid YAML: %v\n%s", err, bundles)
	}
}

// A bundle generated inside a project's PSR-4 tree must not carry a composer.json: composer
// would treat the directory as a nested package and the project's own autoloader already
// covers it.
func TestBundleInsideProjectHasNoComposerJson(t *testing.T) {
	useTemplates(t, testTemplates())

	dest := filepath.Join(t.TempDir(), "src", "Acme", "Bundle", "FooBundle")
	opts := BundleOptions{
		ClassName:   "AcmeFooBundle",
		Namespace:   `Acme\Bundle\FooBundle`,
		Prefix:      "AcmeFoo",
		Alias:       "acme_foo",
		PackageName: "acme/foo-bundle",
	}

	if err := Bundle(dest, opts); err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	for _, f := range []string{"composer.json", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err == nil {
			t.Errorf("%s must not be generated for a bundle inside a project", f)
		}
	}
	// The kernel still has to find it, so bundles.yml is not optional.
	if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash("Resources/config/oro/bundles.yml"))); err != nil {
		t.Errorf("bundles.yml is what makes Oro discover the bundle: %v", err)
	}
}

func TestBundleRefusesNonEmptyDir(t *testing.T) {
	useTemplates(t, testTemplates())

	dest := t.TempDir() // already exists and we put a file in it
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := BundleOptions{ClassName: "AcmeFooBundle", Namespace: "Acme", Prefix: "AcmeFoo", Alias: "acme_foo", PackageName: "acme/foo-bundle", Standalone: true}
	if err := Bundle(dest, opts); err == nil {
		t.Fatal("expected an error scaffolding into a non-empty dir, got nil")
	}

	// Nothing from the skeleton should have been written.
	if _, err := os.Stat(filepath.Join(dest, "composer.json")); err == nil {
		t.Error("skeleton files should not be written into a non-empty dir")
	}
}

func TestResolveBundlePlacement(t *testing.T) {
	opts, err := ParseBundleArg(`Acme\Bundle\FooBundle`, "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("project psr-4 root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"autoload":{"psr-4":{"":"src/"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := ResolveBundlePlacement(root, opts, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("src", "Acme", "Bundle", "FooBundle"); got.Dir != want {
			t.Errorf("dir = %q, want %q", got.Dir, want)
		}
		if got.Standalone {
			t.Error("a bundle under the project's PSR-4 root is not standalone")
		}
		if got.Psr4.Dir != "src" {
			t.Errorf("psr-4 dir = %q, want src", got.Psr4.Dir)
		}
	})

	t.Run("no composer.json falls back to standalone", func(t *testing.T) {
		got, err := ResolveBundlePlacement(t.TempDir(), opts, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Standalone || got.Dir != "AcmeFooBundle" {
			t.Errorf("placement = %+v, want a standalone AcmeFooBundle dir", got)
		}
	})

	t.Run("path override keeps the shape", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"autoload":{"psr-4":{"":"src/"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := ResolveBundlePlacement(root, opts, "custom/place", false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Dir != "custom/place" || got.Standalone {
			t.Errorf("placement = %+v, want custom/place and not standalone", got)
		}
	})

	t.Run("absolute path override is used as given", func(t *testing.T) {
		root := t.TempDir()
		abs := filepath.Join(t.TempDir(), "elsewhere")

		got, err := ResolveBundlePlacement(root, opts, abs, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Dest(root) != abs {
			t.Errorf("dest = %q, want %q", got.Dest(root), abs)
		}
	})
}
