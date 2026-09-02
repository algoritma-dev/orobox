package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/algoritma-dev/orobox/internal/scaffold"
)

// captureCreateBundle stubs the scaffolding step and runs `orobox create bundle` inside a
// throwaway directory: the command resolves the target from the composer.json it finds in the
// working directory, so the directory is the fixture.
//
// The flag variables are package-level and cobra does not reset them between Execute calls, so
// each case restores them; without that a --path from one test silently steered the next.
func captureCreateBundle(t *testing.T, dir string, args ...string) (string, scaffold.BundleOptions) {
	t.Helper()

	oldBundle := scaffoldBundle
	oldPath, oldPkg, oldClass, oldStandalone := createBundlePath, createBundlePackage, createBundleClass, createBundleStandalone
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		scaffoldBundle = oldBundle
		createBundlePath, createBundlePackage, createBundleClass, createBundleStandalone = oldPath, oldPkg, oldClass, oldStandalone
		rootCmd.SetArgs(nil)
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	})

	var gotDest string
	var gotOpts scaffold.BundleOptions
	scaffoldBundle = func(dest string, opts scaffold.BundleOptions) error {
		gotDest, gotOpts = dest, opts
		return nil
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs(append([]string{"create", "bundle"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}
	return gotDest, gotOpts
}

// writeComposer drops a project composer.json carrying the PSR-4 map an OroCommerce
// application ships.
func writeComposer(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Inside an Oro checkout the `"": "src/"` rule is the whole answer: the namespace decides the
// path, and the bundle needs no composer package of its own.
func TestCreateBundleLandsInProjectPsr4Root(t *testing.T) {
	dir := t.TempDir()
	writeComposer(t, dir, `{"autoload":{"psr-4":{"":"src/"}}}`)

	dest, opts := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`)

	// t.TempDir can hand back a symlinked path (/tmp -> /private/tmp on macOS), and the
	// command joins onto os.Getwd(), which is resolved. Compare the resolved forms.
	want := filepath.Join(resolve(t, dir), "src", "Acme", "Bundle", "FooBundle")
	if resolve(t, dest) != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if opts.Standalone {
		t.Error("a bundle inside the project's PSR-4 tree must not be standalone")
	}
	if opts.ClassName != "AcmeFooBundle" || opts.Namespace != `Acme\Bundle\FooBundle` {
		t.Errorf("opts = %+v, want ClassName=AcmeFooBundle Namespace=Acme\\Bundle\\FooBundle", opts)
	}
	if opts.Alias != "acme_foo" {
		t.Errorf("alias = %q, want acme_foo", opts.Alias)
	}
}

// A longer PSR-4 prefix wins over the root one, so a project that maps `Acme\` sends the
// bundle there instead of under src/.
func TestCreateBundleUsesLongestPsr4Prefix(t *testing.T) {
	dir := t.TempDir()
	writeComposer(t, dir, `{"autoload":{"psr-4":{"":"src/","Acme\\":"lib/acme/"}}}`)

	dest, _ := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`)

	want := filepath.Join(resolve(t, dir), "lib", "acme", "Bundle", "FooBundle")
	if resolve(t, dest) != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
}

// With no composer.json there is nothing to autoload the bundle, so it becomes its own
// package in a directory named after the class.
func TestCreateBundleFallsBackToStandalone(t *testing.T) {
	dir := t.TempDir()

	dest, opts := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`)

	want := filepath.Join(resolve(t, dir), "AcmeFooBundle")
	if resolve(t, dest) != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if !opts.Standalone {
		t.Error("a bundle outside a PHP project must be standalone")
	}
	if opts.PackageName != "acme/foo-bundle" {
		t.Errorf("package = %q, want acme/foo-bundle", opts.PackageName)
	}
}

// --standalone ignores a PSR-4 map that would otherwise have claimed the bundle.
func TestCreateBundleStandaloneFlagOverridesPsr4(t *testing.T) {
	dir := t.TempDir()
	writeComposer(t, dir, `{"autoload":{"psr-4":{"":"src/"}}}`)

	dest, opts := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`, "--standalone")

	want := filepath.Join(resolve(t, dir), "AcmeFooBundle")
	if resolve(t, dest) != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if !opts.Standalone {
		t.Error("--standalone must produce a standalone bundle")
	}
}

// --path moves the bundle without changing its shape: it is still the project's, not its own
// package.
func TestCreateBundlePathFlag(t *testing.T) {
	dir := t.TempDir()
	writeComposer(t, dir, `{"autoload":{"psr-4":{"":"src/"}}}`)

	dest, opts := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`, "--path", "custom/place")

	want := filepath.Join(resolve(t, dir), "custom", "place")
	if resolve(t, dest) != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if opts.Standalone {
		t.Error("--path changes the location, not the shape")
	}
}

func TestCreateBundleClassAndPackageOverrides(t *testing.T) {
	dir := t.TempDir()

	_, opts := captureCreateBundle(t, dir, `Acme\Bundle\FooBundle`, "--class", "MyBundle", "--package", "custom/pkg")

	if opts.ClassName != "MyBundle" {
		t.Errorf("class = %q, want MyBundle", opts.ClassName)
	}
	if opts.PackageName != "custom/pkg" {
		t.Errorf("package = %q, want custom/pkg", opts.PackageName)
	}
}

func TestCreateProjectCommand(t *testing.T) {
	oldProject := scaffoldProject
	t.Cleanup(func() {
		scaffoldProject = oldProject
		rootCmd.SetArgs(nil)
	})

	var gotDest, gotVersion string
	scaffoldProject = func(dest, version string) error {
		gotDest, gotVersion = dest, version
		return nil
	}

	rootCmd.SetArgs([]string{"create", "project", "myproj", "--oro-version", "6.1"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if gotDest != "myproj" {
		t.Errorf("dest = %q, want myproj", gotDest)
	}
	if gotVersion != "6.1" {
		t.Errorf("version = %q, want 6.1", gotVersion)
	}
}

func TestCreateBundleRequiresArg(t *testing.T) {
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	rootCmd.SetArgs([]string{"create", "bundle"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error when the bundle namespace arg is missing")
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
