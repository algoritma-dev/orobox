package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	yamlv3 "gopkg.in/yaml.v3"
)

func testTemplates() fstest.MapFS {
	return fstest.MapFS{
		"templates/bundle/bundle.php.tmpl":        {Data: []byte("<?php\nnamespace {{.Namespace}};\nclass {{.ClassName}} extends Bundle {}\n")},
		"templates/bundle/Extension.php.tmpl":     {Data: []byte("<?php\nnamespace {{.Namespace}}\\DependencyInjection;\nclass {{.Prefix}}Extension {}\n")},
		"templates/bundle/Configuration.php.tmpl": {Data: []byte("<?php\nnamespace {{.Namespace}}\\DependencyInjection;\n// tree {{.Alias}}\n")},
		"templates/bundle/services.yml.tmpl":      {Data: []byte("services:\n")},
		"templates/bundle/bundles.yml.tmpl":       {Data: []byte("bundles:\n    - { name: {{.Namespace}}\\{{.ClassName}}, priority: 30 }\n")},
		"templates/bundle/composer.json.tmpl":     {Data: []byte(`{"name":"{{.PackageName}}","type":"symfony-bundle","autoload":{"psr-4":{"{{esc .Namespace}}\\":""}}}`)},
		"templates/bundle/gitignore.tmpl":         {Data: []byte("/vendor/\n/vendor-oro/\n")},
	}
}

func TestBundleWritesSkeleton(t *testing.T) {
	old := Templates
	Templates = testTemplates()
	defer func() { Templates = old }()

	dir := t.TempDir()
	dest := filepath.Join(dir, "AcmeFooBundle")

	opts := BundleOptions{
		ClassName:   "AcmeFooBundle",
		Namespace:   `Acme\FooBundle`,
		Prefix:      "AcmeFoo",
		Alias:       "acme_foo",
		PackageName: "acme/foo-bundle",
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
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}

	classContent, _ := os.ReadFile(filepath.Join(dest, "AcmeFooBundle.php"))
	if !contains(string(classContent), `namespace Acme\FooBundle;`) || !contains(string(classContent), "class AcmeFooBundle") {
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

	bundles, _ := os.ReadFile(filepath.Join(dest, "Resources/config/oro/bundles.yml"))
	var by map[string]any
	if err := yamlv3.Unmarshal(bundles, &by); err != nil {
		t.Errorf("bundles.yml is not valid YAML: %v\n%s", err, bundles)
	}
}

func TestBundleRefusesNonEmptyDir(t *testing.T) {
	old := Templates
	Templates = testTemplates()
	defer func() { Templates = old }()

	dest := t.TempDir() // already exists and we put a file in it
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := BundleOptions{ClassName: "AcmeFooBundle", Namespace: "Acme", Prefix: "AcmeFoo", Alias: "acme_foo", PackageName: "acme/foo-bundle"}
	if err := Bundle(dest, opts); err == nil {
		t.Fatal("expected error scaffolding into a non-empty dir, got nil")
	}

	// Nothing from the skeleton should have been written.
	if _, err := os.Stat(filepath.Join(dest, "composer.json")); err == nil {
		t.Error("skeleton files should not be written into a non-empty dir")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
