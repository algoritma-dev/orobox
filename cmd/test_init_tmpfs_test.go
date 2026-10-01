package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

// `test-init --tmpfs` records the choice in .orobox.yaml. It used to re-marshal the whole config,
// which dropped every comment and rewrote keys the command has nothing to do with; it now touches
// only the two test keys.
func TestPersistTmpfsSettingsKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".orobox.yaml")
	original := `# Team config, reviewed in MR !42
type: project
oro_version: "6.1" # pinned until the 6.1 LTS ends
domains:
  - host: oro.demo
dockerfile: docker/Dockerfile # still the old spelling
php_ini:
  xdebug.log_level: 0
test:
  qa:
    eslint: false
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := persistTmpfsSettings(path, "2g"); err != nil {
		t.Fatalf("persistTmpfsSettings: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{
		"# Team config, reviewed in MR !42",
		"# pinned until the 6.1 LTS ends",
		"dockerfile: docker/Dockerfile # still the old spelling",
		"xdebug.log_level: 0",
	} {
		if !strings.Contains(string(out), keep) {
			t.Errorf("%q lost from .orobox.yaml:\n%s", keep, out)
		}
	}
	if strings.Contains(string(out), "image:") {
		t.Errorf("the deprecated dockerfile key must not be migrated by test-init:\n%s", out)
	}

	conf, err := config.ParseConfig(out)
	if err != nil {
		t.Fatalf("rewritten file does not parse: %v\n%s", err, out)
	}
	if !conf.Test.UseTmpfs || conf.Test.TmpfsSize != "2g" {
		t.Errorf("test = %+v, want use_tmpfs true and tmpfs_size 2g", conf.Test)
	}
	if conf.Test.Qa == nil || conf.Test.Qa.Eslint == nil || *conf.Test.Qa.Eslint {
		t.Errorf("test.qa.eslint must stay false, got %+v", conf.Test.Qa)
	}
}

func TestPersistTmpfsSettingsReportsAMissingFile(t *testing.T) {
	if err := persistTmpfsSettings(filepath.Join(t.TempDir(), "missing.yaml"), "1g"); err == nil {
		t.Error("want an error for a config file that does not exist")
	}
}
