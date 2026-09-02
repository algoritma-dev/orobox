//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostBox is a Box with no Docker behind it: the create commands only touch the filesystem
// (and, for `create project`, git), so they are exercised against a bare working directory
// rather than a matrix case. No teardown is registered because nothing was started.
func hostBox(t *testing.T) *Box {
	t.Helper()
	return &Box{t: t, dir: t.TempDir(), bin: binaryPath(t)}
}

// standaloneBundleFiles is the skeleton `orobox create bundle` writes for a bundle that owns
// its own composer package.
var standaloneBundleFiles = []string{
	"AcmeFooBundle.php",
	"DependencyInjection/AcmeFooExtension.php",
	"DependencyInjection/Configuration.php",
	"Resources/config/services.yml",
	"Resources/config/oro/bundles.yml",
	"composer.json",
	".gitignore",
}

// Outside a PHP project there is nothing to autoload the bundle, so create has to produce a
// self-contained composer package.
func TestCreateBundleStandalone(t *testing.T) {
	box := hostBox(t)

	box.Run("create", "bundle", `Acme\Bundle\FooBundle`)

	dest := filepath.Join(box.Dir(), "AcmeFooBundle")
	for _, rel := range standaloneBundleFiles {
		path := filepath.Join(dest, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("create bundle did not write %s: %v", rel, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("create bundle wrote an empty %s", rel)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dest, "composer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var composer struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Autoload struct {
			Psr4 map[string]string `json:"psr-4"`
		} `json:"autoload"`
	}
	if err := json.Unmarshal(raw, &composer); err != nil {
		t.Fatalf("generated composer.json is not valid JSON: %v\n%s", err, raw)
	}
	if composer.Name != "acme/foo-bundle" {
		t.Errorf("composer name = %q, want acme/foo-bundle", composer.Name)
	}
	if composer.Type != "symfony-bundle" {
		t.Errorf("composer type = %q, want symfony-bundle", composer.Type)
	}
	if _, ok := composer.Autoload.Psr4[`Acme\Bundle\FooBundle\`]; !ok {
		t.Errorf("composer psr-4 = %v, want a key of Acme\\Bundle\\FooBundle\\", composer.Autoload.Psr4)
	}
}

// Scaffolding over an existing tree is the one way create can destroy work, so the guard is
// asserted end to end rather than only in the unit tests.
func TestCreateBundleRefusesANonEmptyTarget(t *testing.T) {
	box := hostBox(t)

	box.Run("create", "bundle", `Acme\Bundle\FooBundle`)
	if res := box.TryRun("create", "bundle", `Acme\Bundle\FooBundle`); !failed(res) {
		t.Errorf("create bundle should refuse a non-empty target, got exit %d\n%s", res.ExitCode, res.Stdout)
	}
}

// The PSR-4 case, host-side: an OroCommerce application autoloads "": "src/", so the
// namespace alone decides the path and the bundle carries no composer.json of its own.
func TestCreateBundleFollowsTheProjectPsr4Map(t *testing.T) {
	box := hostBox(t)
	if err := os.WriteFile(
		filepath.Join(box.Dir(), "composer.json"),
		[]byte(`{"name":"acme/app","autoload":{"psr-4":{"":"src/"}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	box.Run("create", "bundle", `Acme\Bundle\FooBundle`)

	dest := filepath.Join(box.Dir(), "src", "Acme", "Bundle", "FooBundle")
	if _, err := os.Stat(filepath.Join(dest, "AcmeFooBundle.php")); err != nil {
		t.Fatalf("create bundle ignored the project's PSR-4 map: %v", err)
	}
	for _, rel := range []string{"composer.json", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err == nil {
			t.Errorf("a bundle inside the project's PSR-4 tree must not carry its own %s", rel)
		}
	}

	class, err := os.ReadFile(filepath.Join(dest, "AcmeFooBundle.php"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(class), `namespace Acme\Bundle\FooBundle;`) {
		t.Errorf("generated class has the wrong namespace:\n%s", class)
	}
}

// --path relocates the bundle but leaves it part of the project.
func TestCreateBundlePathFlag(t *testing.T) {
	box := hostBox(t)
	if err := os.WriteFile(
		filepath.Join(box.Dir(), "composer.json"),
		[]byte(`{"name":"acme/app","autoload":{"psr-4":{"":"src/"}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	box.Run("create", "bundle", `Acme\Bundle\FooBundle`, "--path", "custom/place")

	if _, err := os.Stat(filepath.Join(box.Dir(), "custom", "place", "AcmeFooBundle.php")); err != nil {
		t.Fatalf("--path did not move the bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(box.Dir(), "custom", "place", "composer.json")); err == nil {
		t.Error("--path changes the location, not the shape: no composer.json belongs here")
	}
}

// TestCreateProjectClonesTheOroApplication is the one create case that reaches the network:
// it clones the public OroCommerce application skeleton.
//
// It also pins the premise the whole PSR-4 placement rests on — the application's own
// composer.json maps "" to src/. If Oro ever changes that, `create bundle` starts putting
// bundles somewhere the project does not autoload, and this is the test that says so.
func TestCreateProjectClonesTheOroApplication(t *testing.T) {
	if os.Getenv("E2E_SKIP_CLONE") != "" {
		t.Skip("E2E_SKIP_CLONE is set")
	}
	box := hostBox(t)

	version := createProjectVersion()
	box.Run("create", "project", "app", "--oro-version", version)

	dest := filepath.Join(box.Dir(), "app")
	raw, err := os.ReadFile(filepath.Join(dest, "composer.json"))
	if err != nil {
		t.Fatalf("clone produced no composer.json: %v", err)
	}
	var composer struct {
		Autoload struct {
			Psr4 map[string]json.RawMessage `json:"psr-4"`
		} `json:"autoload"`
	}
	if err := json.Unmarshal(raw, &composer); err != nil {
		t.Fatalf("cloned composer.json is not valid JSON: %v", err)
	}
	root, ok := composer.Autoload.Psr4[""]
	if !ok {
		t.Fatalf("OroCommerce %s no longer maps the empty PSR-4 prefix; create bundle's placement rule is stale. psr-4 = %v",
			version, composer.Autoload.Psr4)
	}
	if !strings.Contains(string(root), "src/") {
		t.Errorf(`OroCommerce %s maps "" to %s, not src/`, version, root)
	}

	// A fresh history is the point of the strip: the user's first commit is their own.
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Error("create project must strip the cloned .git")
	}

	if res := box.TryRun("create", "project", "app"); !failed(res) {
		t.Errorf("create project should refuse a non-empty target, got exit %d\n%s", res.ExitCode, res.Stdout)
	}
}

// createProjectVersion picks the OroCommerce version the clone test uses: the first entry of
// E2E_VERSIONS when the matrix is pinned, so a run scoped to one version does not go and
// resolve tags for another.
func createProjectVersion() string {
	cases, err := ParseMatrix(os.Getenv("E2E_VERSIONS"), string(TypeProject))
	if err != nil || len(cases) == 0 {
		return "6.1"
	}
	return cases[0].Version
}
