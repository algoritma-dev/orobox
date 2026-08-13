package config

import (
	"strings"
	"testing"
)

func projectConfigWithStages(stages ...StageConfig) *OroConfig {
	return &OroConfig{
		Type:       InstallTypeProject,
		OroVersion: "6.1",
		Domains:    []DomainConfig{{Host: "oro.demo", Root: "public"}},
		Deploy:     &DeployConfig{Stages: stages},
	}
}

func validStage() StageConfig {
	return StageConfig{
		Name:       "staging",
		Ref:        "develop",
		Host:       "staging.acme.com",
		User:       "deploy",
		DeployPath: "/var/www/oro",
	}
}

func TestParseConfigWithDeployBlock(t *testing.T) {
	data := []byte(`type: project
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
    ssl: true
deploy:
  pre_built_assets_enabled: true
  repository: git@gitlab.com:acme/shop.git
  stages:
    - name: staging
      ref: develop
      host: staging.acme.com
      user: deploy
      port: 2222
      deploy_path: /var/www/oro
      keep_releases: 3
      test_suites: [unit, functional]
      restart_command: sudo systemctl restart oro-consumer
`)

	conf, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if err := conf.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !conf.Deploy.PreBuiltAssetsEnabled {
		t.Error("PreBuiltAssetsEnabled = false, want true")
	}
	if conf.Deploy.Repository != "git@gitlab.com:acme/shop.git" {
		t.Errorf("Repository = %q", conf.Deploy.Repository)
	}

	stage, err := conf.Deploy.StageFor("staging")
	if err != nil {
		t.Fatalf("StageFor() error = %v", err)
	}
	if stage.SSHPort() != 2222 {
		t.Errorf("SSHPort() = %d, want 2222", stage.SSHPort())
	}
	if stage.Releases() != 3 {
		t.Errorf("Releases() = %d, want 3", stage.Releases())
	}
	if !stage.RunsFunctionalTests() {
		t.Error("RunsFunctionalTests() = false, want true")
	}
	if stage.RestartCommand == "" {
		t.Error("RestartCommand is empty")
	}
}

func TestStageDefaults(t *testing.T) {
	stage := validStage()

	if got := stage.SSHPort(); got != DefaultSSHPort {
		t.Errorf("SSHPort() = %d, want %d", got, DefaultSSHPort)
	}
	if got := stage.Releases(); got != DefaultKeepReleases {
		t.Errorf("Releases() = %d, want %d", got, DefaultKeepReleases)
	}
	if got := stage.Suites(); len(got) != 1 || got[0] != TestSuiteUnit {
		t.Errorf("Suites() = %v, want [%s]", got, TestSuiteUnit)
	}
	if stage.RunsFunctionalTests() {
		t.Error("RunsFunctionalTests() = true, want false for the default suites")
	}
}

func TestStageForResolution(t *testing.T) {
	single := DeployConfig{Stages: []StageConfig{validStage()}}
	if _, err := single.StageFor(""); err != nil {
		t.Errorf("StageFor(\"\") with one stage error = %v, want nil", err)
	}

	production := validStage()
	production.Name = "production"
	multi := DeployConfig{Stages: []StageConfig{validStage(), production}}

	if _, err := multi.StageFor(""); err == nil {
		t.Error("StageFor(\"\") with two stages error = nil, want an error listing the names")
	} else if !strings.Contains(err.Error(), "production") {
		t.Errorf("error %q does not list the configured stages", err)
	}

	if _, err := multi.StageFor("nope"); err == nil {
		t.Error("StageFor(\"nope\") error = nil, want an unknown stage error")
	}

	if _, err := (&DeployConfig{}).StageFor("staging"); err == nil {
		t.Error("StageFor on an empty deploy config error = nil, want an error pointing at deploy-init")
	}
}

func TestValidateDeploy(t *testing.T) {
	noRef := validStage()
	noRef.Ref = ""

	duplicate := validStage()

	badSuite := validStage()
	badSuite.TestSuites = []string{"unit", "integration"}

	tests := []struct {
		name    string
		conf    *OroConfig
		wantErr string
	}{
		{
			name: "valid",
			conf: projectConfigWithStages(validStage()),
		},
		{
			name: "absent block",
			conf: &OroConfig{Type: InstallTypeBundle, Namespace: "Acme", OroVersion: "6.1", Domains: []DomainConfig{{Host: "oro.demo"}}},
		},
		{
			name:    "missing required field",
			conf:    projectConfigWithStages(noRef),
			wantErr: `missing required field "ref"`,
		},
		{
			name:    "duplicate stage name",
			conf:    projectConfigWithStages(validStage(), duplicate),
			wantErr: "duplicate deploy stage name",
		},
		{
			name:    "unknown test suite",
			conf:    projectConfigWithStages(badSuite),
			wantErr: `unknown test suite "integration"`,
		},
		{
			name: "bundle install type",
			conf: func() *OroConfig {
				c := projectConfigWithStages(validStage())
				c.Type = InstallTypeBundle
				c.Namespace = "Acme"
				return c
			}(),
			wantErr: "only supported for install type",
		},
		{
			name: "demo install type",
			conf: func() *OroConfig {
				c := projectConfigWithStages(validStage())
				c.Type = InstallTypeDemo
				return c
			}(),
			wantErr: "only supported for install type",
		},
		{
			name: "source_dir inside the repository",
			conf: func() *OroConfig {
				c := projectConfigWithStages(validStage())
				c.Deploy.SourceDir = "b2b"
				return c
			}(),
		},
		{
			name: "absolute source_dir",
			conf: func() *OroConfig {
				c := projectConfigWithStages(validStage())
				c.Deploy.SourceDir = "/var/www/oro"
				return c
			}(),
			wantErr: "must be relative to the repository root",
		},
		{
			name: "escaping source_dir",
			conf: func() *OroConfig {
				c := projectConfigWithStages(validStage())
				c.Deploy.SourceDir = "../b2b"
				return c
			}(),
			wantErr: "must be a directory inside the repository",
		},
		{
			name: "settings without stages",
			conf: &OroConfig{
				Type:       InstallTypeProject,
				OroVersion: "6.1",
				Domains:    []DomainConfig{{Host: "oro.demo"}},
				Deploy:     &DeployConfig{Repository: "git@gitlab.com:acme/shop.git"},
			},
			wantErr: "at least one entry under 'stages'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.conf.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDeploySourceNormalization(t *testing.T) {
	tests := map[string]string{
		"":          "",
		".":         "",
		"b2b":       "b2b",
		"/b2b/":     "b2b",
		"  b2b  ":   "b2b",
		"src/./api": "src/api",
		"../b2b":    "../b2b",
	}

	for raw, want := range tests {
		d := DeployConfig{SourceDir: raw}
		if got := d.Source(); got != want {
			t.Errorf("Source() for %q = %q, want %q", raw, got, want)
		}
	}

	var nilConf *DeployConfig
	if got := nilConf.Source(); got != "" {
		t.Errorf("Source() on a nil config = %q, want empty", got)
	}
}

func TestResolveRepositoryPrefersConfig(t *testing.T) {
	d := DeployConfig{Repository: "git@gitlab.com:acme/shop.git"}
	got, err := d.ResolveRepository()
	if err != nil {
		t.Fatalf("ResolveRepository() error = %v", err)
	}
	if got != "git@gitlab.com:acme/shop.git" {
		t.Errorf("ResolveRepository() = %q", got)
	}
}
