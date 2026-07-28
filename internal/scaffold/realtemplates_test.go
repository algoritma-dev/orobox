package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

// TestRealBundleTemplatesRender renders the templates actually shipped under
// templates/bundle and asserts the generated composer.json and bundles.yml are valid.
func TestRealBundleTemplatesRender(t *testing.T) {
	old := Templates
	// Repo root relative to this package directory (internal/scaffold).
	Templates = os.DirFS(filepath.Join("..", ".."))
	defer func() { Templates = old }()

	dest := filepath.Join(t.TempDir(), "AcmeFooBundle")
	opts, err := ParseBundleArg(`Acme\FooBundle\AcmeFooBundle`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := Bundle(dest, opts); err != nil {
		t.Fatalf("Bundle with real templates: %v", err)
	}

	composer, err := os.ReadFile(filepath.Join(dest, "composer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cj map[string]any
	if err := json.Unmarshal(composer, &cj); err != nil {
		t.Errorf("real composer.json invalid JSON: %v\n%s", err, composer)
	}
	if cj["name"] != "acme/foo-bundle" {
		t.Errorf("composer name = %v, want acme/foo-bundle", cj["name"])
	}

	bundles, err := os.ReadFile(filepath.Join(dest, "Resources/config/oro/bundles.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var by any
	if err := yamlv3.Unmarshal(bundles, &by); err != nil {
		t.Errorf("real bundles.yml invalid YAML: %v\n%s", err, bundles)
	}

	class, err := os.ReadFile(filepath.Join(dest, "AcmeFooBundle.php"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(class), `namespace Acme\FooBundle;`) {
		t.Errorf("bundle class missing namespace:\n%s", class)
	}
}
