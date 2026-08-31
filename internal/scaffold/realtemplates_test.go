package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

func TestRealBundleTemplatesRenderStandalone(t *testing.T) {
	useRealTemplates(t)

	dest := filepath.Join(t.TempDir(), "AcmeFooBundle")
	opts, err := ParseBundleArg(`Acme\Bundle\FooBundle`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	opts.Standalone = true

	if err := Bundle(dest, opts); err != nil {
		t.Fatalf("Bundle with real templates: %v", err)
	}

	composer, err := os.ReadFile(filepath.Join(dest, "composer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cj struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Autoload struct {
			Psr4 map[string]string `json:"psr-4"`
		} `json:"autoload"`
	}
	if err := json.Unmarshal(composer, &cj); err != nil {
		t.Fatalf("real composer.json invalid JSON: %v\n%s", err, composer)
	}
	if cj.Name != "acme/foo-bundle" {
		t.Errorf("composer name = %q, want acme/foo-bundle", cj.Name)
	}
	// The `esc` helper is what keeps the namespace a legal JSON string; if it regresses the
	// document above stops parsing, and if it over-escapes the key stops being the namespace.
	if _, ok := cj.Autoload.Psr4[`Acme\Bundle\FooBundle\`]; !ok {
		t.Errorf("composer psr-4 = %v, want a key of Acme\\Bundle\\FooBundle\\", cj.Autoload.Psr4)
	}

	bundles, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash("Resources/config/oro/bundles.yml")))
	if err != nil {
		t.Fatal(err)
	}
	var by struct {
		Bundles []struct {
			Name     string `yaml:"name"`
			Priority int    `yaml:"priority"`
		} `yaml:"bundles"`
	}
	if err := yamlv3.Unmarshal(bundles, &by); err != nil {
		t.Fatalf("real bundles.yml invalid YAML: %v\n%s", err, bundles)
	}
	if len(by.Bundles) != 1 || by.Bundles[0].Name != `Acme\Bundle\FooBundle\AcmeFooBundle` {
		t.Errorf("bundles.yml = %+v, want the fully qualified bundle class", by.Bundles)
	}

	class, err := os.ReadFile(filepath.Join(dest, "AcmeFooBundle.php"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(class), `namespace Acme\Bundle\FooBundle;`) {
		t.Errorf("bundle class missing namespace:\n%s", class)
	}

	extension, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash("DependencyInjection/AcmeFooExtension.php")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(extension), "class AcmeFooExtension") {
		t.Errorf("extension class name wrong:\n%s", extension)
	}

	config, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash("DependencyInjection/Configuration.php")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `new TreeBuilder('acme_foo')`) {
		t.Errorf("configuration tree name wrong:\n%s", config)
	}
}

// The in-project shape is the default one inside an Oro checkout, so the real templates have
// to produce a complete bundle without the composer package files.
func TestRealBundleTemplatesRenderInsideProject(t *testing.T) {
	useRealTemplates(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"autoload":{"psr-4":{"":"src/"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	opts, err := ParseBundleArg(`Acme\Bundle\FooBundle`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := ResolveBundlePlacement(root, opts, "", false)
	if err != nil {
		t.Fatal(err)
	}
	opts.Standalone = placement.Standalone

	if err := Bundle(placement.Dest(root), opts); err != nil {
		t.Fatalf("Bundle with real templates: %v", err)
	}

	dest := filepath.Join(root, "src", "Acme", "Bundle", "FooBundle")
	if _, err := os.Stat(filepath.Join(dest, "AcmeFooBundle.php")); err != nil {
		t.Fatalf("bundle class not written where the PSR-4 map points: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "composer.json")); err == nil {
		t.Error("a bundle inside the project's PSR-4 tree must not carry its own composer.json")
	}
}
