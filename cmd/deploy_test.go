package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/pipeline"
	"github.com/spf13/viper"
)

func init() {
	// The deploy templates are rendered from the repository root, one level up from cmd.
	pipeline.Templates = os.DirFS("..")
}

// setDeployConfig loads a YAML document into viper the way initConfig does at runtime.
func setDeployConfig(t *testing.T, yaml string) {
	t.Helper()
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("could not load test config: %v", err)
	}
	t.Cleanup(viper.Reset)
}

const projectDeployYAML = `
type: project
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
deploy:
  pre_built_assets_enabled: false
  repository: git@gitlab.com:acme/shop.git
  stages:
    - name: staging
      ref: develop
      host: staging.acme.com
      user: deploy
      deploy_path: /var/www/oro
    - name: production
      ref: main
      host: acme.com
      user: deploy
      deploy_path: /var/www/oro
      test_suites: [unit, functional]
`

func TestLoadDeployConfig(t *testing.T) {
	setDeployConfig(t, projectDeployYAML)

	conf, err := loadDeployConfig()
	if err != nil {
		t.Fatalf("loadDeployConfig() error = %v", err)
	}
	if got := conf.Deploy.StageNames(); len(got) != 2 || got[1] != "production" {
		t.Errorf("stage names = %v", got)
	}
}

func TestLoadDeployConfigRejectsNonProject(t *testing.T) {
	setDeployConfig(t, `
type: bundle
namespace: Acme
oro_version: "6.1"
domains:
  - host: oro.demo
`)

	_, err := loadDeployConfig()
	if err == nil {
		t.Fatal("loadDeployConfig() error = nil, want a rejection for the bundle install type")
	}
	if !strings.Contains(err.Error(), "only available for install type") {
		t.Errorf("error = %q", err)
	}
}

func TestLoadDeployConfigRequiresDeployBlock(t *testing.T) {
	setDeployConfig(t, `
type: project
oro_version: "6.1"
domains:
  - host: oro.demo
`)

	_, err := loadDeployConfig()
	if err == nil || !strings.Contains(err.Error(), "deploy-init") {
		t.Errorf("error = %v, want a pointer to deploy-init", err)
	}
}

func TestStageSelection(t *testing.T) {
	setDeployConfig(t, projectDeployYAML)

	conf, err := loadDeployConfig()
	if err != nil {
		t.Fatalf("loadDeployConfig() error = %v", err)
	}

	if _, err := conf.Deploy.StageFor(""); err == nil {
		t.Error("an empty stage name must be rejected when two stages exist")
	}
	stage, err := conf.Deploy.StageFor("production")
	if err != nil {
		t.Fatalf("StageFor(production) error = %v", err)
	}
	if !stage.RunsFunctionalTests() {
		t.Error("production stage should run functional tests")
	}
}

func TestCheckDeployerFiles(t *testing.T) {
	dir := t.TempDir()

	if err := checkDeployerFiles(dir); err == nil || !strings.Contains(err.Error(), "deploy-init") {
		t.Errorf("error = %v, want a missing deploy.php error pointing at deploy-init", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "deploy.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkDeployerFiles(dir); err == nil || !strings.Contains(err.Error(), "vendor-bin/deploy/composer.json") {
		t.Errorf("error = %v, want the missing composer.json to be reported", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "vendor-bin", "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor-bin", "deploy", "composer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkDeployerFiles(dir); err != nil {
		t.Errorf("checkDeployerFiles() error = %v, want nil once both files exist", err)
	}
}

func TestDeployOptionsRequiresCredentials(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("OROBOX_DEPLOY_SSH_KEY", "")
	t.Setenv("CI_JOB_TOKEN", "")

	if _, err := deployOptions(t.TempDir(), config.StageConfig{Host: "acme.com"}, true); err == nil {
		t.Error("deployOptions() error = nil, want a missing credentials error")
	}
}

func TestDeployOptionsUsesCIJobToken(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("OROBOX_DEPLOY_SSH_KEY", "-----BEGIN KEY-----")
	t.Setenv("OROBOX_DEPLOY_GIT_TOKEN", "")
	t.Setenv("OROBOX_DEPLOY_GIT_USER", "")
	t.Setenv("CI_JOB_TOKEN", "job-token")

	opts, err := deployOptions(t.TempDir(), config.StageConfig{Host: "acme.com"}, true)
	if err != nil {
		t.Fatalf("deployOptions() error = %v", err)
	}
	if opts.GitHTTPToken != "job-token" || opts.GitHTTPUser != "gitlab-ci-token" {
		t.Errorf("git credentials = %q / %q, want the GitLab job token", opts.GitHTTPToken, opts.GitHTTPUser)
	}
	if opts.SSHPrivateKey == "" {
		t.Error("SSHPrivateKey was not picked up from the environment")
	}
}

func TestDeployOptionsSkipsTheCredentialCheckWithoutARelease(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("OROBOX_DEPLOY_SSH_KEY", "")
	t.Setenv("CI_JOB_TOKEN", "")

	if _, err := deployOptions(t.TempDir(), config.StageConfig{Host: "acme.com"}, false); err != nil {
		t.Errorf("deployOptions() error = %v, want nil when the release is skipped", err)
	}
}

func TestSplitSuites(t *testing.T) {
	tests := []struct {
		answer string
		want   []string
	}{
		{"unit, functional", []string{"unit", "functional"}},
		{"functional", []string{"functional"}},
		{"", []string{"unit"}},
		{"unit, integration", []string{"unit"}},
	}

	for _, tt := range tests {
		got := splitSuites(tt.answer)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("splitSuites(%q) = %v, want %v", tt.answer, got, tt.want)
		}
	}
}

func TestWriteDeployFilesKeepsExistingStub(t *testing.T) {
	dir := t.TempDir()

	if err := writeDeployFiles(dir); err != nil {
		t.Fatalf("writeDeployFiles() error = %v", err)
	}

	recipe := filepath.Join(dir, config.DeployRecipeRelPath)
	if data, err := os.ReadFile(recipe); err != nil {
		t.Fatalf("recipe not written: %v", err)
	} else if !strings.Contains(string(data), "task('oro:update'") {
		t.Error("recipe does not contain the Oro tasks")
	}

	stub := filepath.Join(dir, config.DeployStubRelPath)
	if err := os.WriteFile(stub, []byte("<?php // mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The recipe is regenerated on every run; the stub belongs to the project.
	if err := os.WriteFile(recipe, []byte("<?php // stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeDeployFiles(dir); err != nil {
		t.Fatalf("writeDeployFiles() second run error = %v", err)
	}

	if data, _ := os.ReadFile(stub); string(data) != "<?php // mine" {
		t.Errorf("deploy.php was overwritten: %q", data)
	}
	if data, _ := os.ReadFile(recipe); strings.Contains(string(data), "stale") {
		t.Error("the recipe was not refreshed")
	}
}

func TestAskStageDefaultsAndOverrides(t *testing.T) {
	original := stdin
	defer func() { stdin = original }()

	// name, ref, host, user, path, port, keep, suites, restart
	stdin = strings.NewReader("staging\n\nstaging.acme.com\n\n\n2222\n3\nunit, functional\nsudo systemctl restart oro-consumer\n")

	stage := askStage(bufio.NewReader(stdin), config.StageConfig{})

	if stage.Name != "staging" || stage.Host != "staging.acme.com" {
		t.Errorf("stage = %+v", stage)
	}
	if stage.Ref != "main" || stage.User != "deploy" || stage.DeployPath != "/var/www/oro" {
		t.Errorf("defaults were not applied: %+v", stage)
	}
	if stage.Port != 2222 || stage.KeepReleases != 3 {
		t.Errorf("port/keep_releases = %d/%d", stage.Port, stage.KeepReleases)
	}
	if len(stage.TestSuites) != 2 {
		t.Errorf("TestSuites = %v", stage.TestSuites)
	}
	if stage.RestartCommand == "" {
		t.Error("RestartCommand was not read")
	}
}

func TestDeployCommandHasNoCacheFlag(t *testing.T) {
	if deployCmd.Flags().Lookup("no-cache") == nil {
		t.Error("deploy is missing the --no-cache flag")
	}
}

func TestDeployCommandHasSkipFlags(t *testing.T) {
	for _, name := range []string{"skip-qa", "skip-test", "skip-release"} {
		if deployCmd.Flags().Lookup(name) == nil {
			t.Errorf("deploy is missing the --%s flag", name)
		}
	}
}
