package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestInstallTypeFor(t *testing.T) {
	tests := []struct {
		name     string
		wantName string
		wantErr  bool
	}{
		{"", InstallTypeBundle, false},
		{"bundle", InstallTypeBundle, false},
		{"project", InstallTypeProject, false},
		{"demo", "", true},
		{"garbage", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, err := InstallTypeFor(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("InstallTypeFor(%q) err = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if it.Name() != tt.wantName {
				t.Errorf("Name() = %q, want %q", it.Name(), tt.wantName)
			}
		})
	}
}

func TestBundleTypeFlags(t *testing.T) {
	it := bundleType{}
	if it.ImageSuffix() != "bundle" {
		t.Errorf("ImageSuffix() = %q, want bundle", it.ImageSuffix())
	}
	if !it.RunsComposerRequire() {
		t.Error("bundle should RunsComposerRequire")
	}
	if it.RunsComposerInstall() {
		t.Error("bundle should not RunsComposerInstall")
	}
	if !it.SyncsVendorToHost() {
		t.Error("bundle should SyncsVendorToHost")
	}
	if !it.RequiresBundleNamespace() {
		t.Error("bundle should RequiresBundleNamespace")
	}
	if !it.WarnOnMissingComposerJSON() {
		t.Error("bundle should WarnOnMissingComposerJSON")
	}
	if it.BindWholeRepo() {
		t.Error("bundle should not BindWholeRepo")
	}
}

func TestProjectTypeFlags(t *testing.T) {
	it := projectType{}
	if it.ImageSuffix() != "project" {
		t.Errorf("ImageSuffix() = %q, want project", it.ImageSuffix())
	}
	if it.RunsComposerRequire() {
		t.Error("project should not RunsComposerRequire")
	}
	if !it.RunsComposerInstall() {
		t.Error("project should RunsComposerInstall")
	}
	if it.SyncsVendorToHost() {
		t.Error("project should not SyncsVendorToHost")
	}
	if it.RequiresBundleNamespace() {
		t.Error("project should not RequiresBundleNamespace")
	}
	if it.WarnOnMissingComposerJSON() {
		t.Error("project should not WarnOnMissingComposerJSON")
	}
	if !it.BindWholeRepo() {
		t.Error("project should BindWholeRepo")
	}
}

func TestSourceRootContainer(t *testing.T) {
	viper.Reset()
	viper.Set("namespace", "Acme\\FooBundle")

	if got := (projectType{}).SourceRootContainer(); got != OroRootDir {
		t.Errorf("project SourceRootContainer() = %q, want %q", got, OroRootDir)
	}

	want := OroRootDir + "/bundles/Acme/FooBundle"
	if got := (bundleType{}).SourceRootContainer(); got != want {
		t.Errorf("bundle SourceRootContainer() = %q, want %q", got, want)
	}
}

func TestGetSourceRootContainerPath(t *testing.T) {
	viper.Reset()
	viper.Set("namespace", "Acme\\FooBundle")

	viper.Set("type", "project")
	if got := GetSourceRootContainerPath(); got != OroRootDir {
		t.Errorf("project GetSourceRootContainerPath() = %q, want %q", got, OroRootDir)
	}

	viper.Set("type", "bundle")
	want := OroRootDir + "/bundles/Acme/FooBundle"
	if got := GetSourceRootContainerPath(); got != want {
		t.Errorf("bundle GetSourceRootContainerPath() = %q, want %q", got, want)
	}

	// Unknown type falls back to bundle semantics (Validate surfaces the error).
	viper.Set("type", "garbage")
	if got := GetSourceRootContainerPath(); got != want {
		t.Errorf("unknown GetSourceRootContainerPath() = %q, want %q", got, want)
	}
}
