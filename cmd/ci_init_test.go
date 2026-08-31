package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/scaffold"
)

func init() {
	// The CI templates are rendered from the repository root, one level up from cmd.
	scaffold.Templates = os.DirFS("..")
}

const projectCIYAML = `
type: project
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
deploy:
  pre_built_assets_enabled: false
  repository: git@gitlab.com:acme/shop.git
  stages:
    - name: production
      ref: main
      host: acme.com
      user: deploy
      deploy_path: /var/www/oro
`

func TestWriteCIFilesGeneratesBothPipelines(t *testing.T) {
	setDeployConfig(t, projectCIYAML)

	conf, err := loadDeployConfig()
	if err != nil {
		t.Fatalf("loadDeployConfig() error = %v", err)
	}

	dir := t.TempDir()
	if err := writeCIFiles(dir, conf); err != nil {
		t.Fatalf("writeCIFiles() error = %v", err)
	}

	include := filepath.Join(dir, config.CIIncludeRelPath)
	data, err := os.ReadFile(include)
	if err != nil {
		t.Fatalf("%s not written: %v", config.CIIncludeRelPath, err)
	}
	if !strings.Contains(string(data), "orobox:deploy:production:") {
		t.Error("the generated pipeline has no production deploy job")
	}

	root := filepath.Join(dir, config.CIRootRelPath)
	if data, err := os.ReadFile(root); err != nil {
		t.Fatalf("%s not written: %v", config.CIRootRelPath, err)
	} else if !strings.Contains(string(data), config.CIIncludeRelPath) {
		t.Error("the generated root pipeline does not include the Orobox one")
	}
}

func TestWriteCIFilesRewritesOnlyTheOroboxPipeline(t *testing.T) {
	setDeployConfig(t, projectCIYAML)

	conf, err := loadDeployConfig()
	if err != nil {
		t.Fatalf("loadDeployConfig() error = %v", err)
	}

	dir := t.TempDir()
	if err := writeCIFiles(dir, conf); err != nil {
		t.Fatalf("writeCIFiles() error = %v", err)
	}

	root := filepath.Join(dir, config.CIRootRelPath)
	include := filepath.Join(dir, config.CIIncludeRelPath)
	if err := os.WriteFile(root, []byte("# mine\ninclude:\n  - local: "+config.CIIncludeRelPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte("# stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCIFiles(dir, conf); err != nil {
		t.Fatalf("second writeCIFiles() error = %v", err)
	}

	if data, _ := os.ReadFile(root); !strings.Contains(string(data), "# mine") {
		t.Errorf("%s was overwritten: %q", config.CIRootRelPath, data)
	}
	if data, _ := os.ReadFile(include); strings.Contains(string(data), "stale") {
		t.Errorf("%s was not refreshed", config.CIIncludeRelPath)
	}
}

func TestIncludesOroboxPipeline(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, config.CIRootRelPath)

	// A missing file cannot be missing the include: nothing to warn about, ci-init writes it.
	if found, err := includesOroboxPipeline(root); err != nil || found {
		t.Errorf("missing file: found = %v, err = %v", found, err)
	}

	if err := os.WriteFile(root, []byte("stages: [test]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := includesOroboxPipeline(root); err != nil || found {
		t.Errorf("pipeline without the include: found = %v, err = %v", found, err)
	}

	if err := os.WriteFile(root, []byte("include:\n  - local: "+config.CIIncludeRelPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := includesOroboxPipeline(root); err != nil || !found {
		t.Errorf("pipeline with the include: found = %v, err = %v", found, err)
	}
}
