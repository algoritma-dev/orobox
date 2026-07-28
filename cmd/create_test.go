package cmd

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/scaffold"
)

func TestCreateBundleCommand(t *testing.T) {
	oldBundle := scaffoldBundle
	defer func() { scaffoldBundle = oldBundle }()

	var gotDest string
	var gotOpts scaffold.BundleOptions
	scaffoldBundle = func(dest string, opts scaffold.BundleOptions) error {
		gotDest = dest
		gotOpts = opts
		return nil
	}

	rootCmd.SetArgs([]string{"create", "bundle", `Acme\FooBundle\AcmeFooBundle`})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if gotDest != "AcmeFooBundle" {
		t.Errorf("dest = %q, want AcmeFooBundle", gotDest)
	}
	if gotOpts.ClassName != "AcmeFooBundle" || gotOpts.Namespace != `Acme\FooBundle` {
		t.Errorf("opts = %+v, want ClassName=AcmeFooBundle Namespace=Acme\\FooBundle", gotOpts)
	}
	if gotOpts.PackageName != "acme/foo-bundle" {
		t.Errorf("package = %q, want acme/foo-bundle", gotOpts.PackageName)
	}
}

func TestCreateBundleCommandDirFlag(t *testing.T) {
	oldBundle := scaffoldBundle
	defer func() { scaffoldBundle = oldBundle }()

	var gotDest string
	scaffoldBundle = func(dest string, _ scaffold.BundleOptions) error {
		gotDest = dest
		return nil
	}

	rootCmd.SetArgs([]string{"create", "bundle", "AcmeFooBundle", "--dir", "custom/place"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}
	if gotDest != "custom/place" {
		t.Errorf("dest = %q, want custom/place", gotDest)
	}
}

func TestCreateProjectCommand(t *testing.T) {
	oldProject := scaffoldProject
	defer func() { scaffoldProject = oldProject }()

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
	rootCmd.SetArgs([]string{"create", "bundle"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error when bundle class arg is missing")
	}
}
