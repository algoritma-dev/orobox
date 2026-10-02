package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestGetVersionsForOro(t *testing.T) {
	tests := []struct {
		version string
		wantPHP string
	}{
		{"7.0", "8.5"},
		{"6.1", "8.4"},
		{"6.0", "8.3"},
		{"5.1", "8.2"},
		{"7.1", "8.5"}, // fallback to 7.0
		{"6.2", "8.4"}, // fallback to 6.1
		{"4.0", "8.2"}, // fallback to 5.1
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := GetVersionsForOro(tt.version)
			if got.PHP != tt.wantPHP {
				t.Errorf("GetVersionsForOro(%v).PHP = %v, want %v", tt.version, got.PHP, tt.wantPHP)
			}
		})
	}
}

func TestGetQaSymfonyConstraints(t *testing.T) {
	tests := []struct {
		version      string
		wantContains []string
		wantAbsent   []string
	}{
		{
			version:      "5.1",
			wantContains: []string{"symfony/console:^5.4", "symfony/event-dispatcher:^5.4", "symfony/service-contracts:^2.5", "psr/container:^1.1", "psr/log:^2"},
			wantAbsent:   []string{"symfony/service-contracts:^3.0"},
		},
		{
			version:      "7.0",
			wantContains: []string{"symfony/console:^6.4", "symfony/service-contracts:^3.0", "psr/container:^2.0"},
			// psr/log is only pinned on the 5.4 line (console 5.4 conflicts with psr/log >=3).
			wantAbsent: []string{"psr/log:^2", "symfony/service-contracts:^2.5"},
		},
		{
			version:      "6.1",
			wantContains: []string{"symfony/console:^6.4", "symfony/service-contracts:^3.0"},
			wantAbsent:   []string{"psr/log:^2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := strings.Join(GetQaSymfonyConstraints(tt.version), " ")
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("GetQaSymfonyConstraints(%q) missing %q; got %q", tt.version, want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("GetQaSymfonyConstraints(%q) should not contain %q; got %q", tt.version, absent, got)
				}
			}
		})
	}
}

func TestOroConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  OroConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: OroConfig{
				Namespace:  "MyNamespace",
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing namespace",
			config: OroConfig{
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "project without namespace is valid",
			config: OroConfig{
				Type:       InstallTypeProject,
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "demo without namespace is valid",
			config: OroConfig{
				Type:       InstallTypeDemo,
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "unknown type is rejected",
			config: OroConfig{
				Type:       "garbage",
				Namespace:  "MyNamespace",
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing oro_version",
			config: OroConfig{
				Namespace: "MyNamespace",
				Domains: []DomainConfig{
					{Host: "example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing domains",
			config: OroConfig{
				Namespace:  "MyNamespace",
				OroVersion: "6.1",
				Domains:    []DomainConfig{},
			},
			wantErr: true,
		},
		{
			name: "domain missing host",
			config: OroConfig{
				Namespace:  "MyNamespace",
				OroVersion: "6.1",
				Domains: []DomainConfig{
					{Host: ""},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.config.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	yamlData := `
namespace: MyNamespace
oro_version: "6.1"
domains:
  - host: example.com
    ssl: true
services:
  mailpit: true
commands:
  - name: "otr"
    command: "php bin/console oro:test:run"
    description: "Runs the Shippy Pro tests suite"
    service: "test-app"
`
	config, err := ParseConfig([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if config.Namespace != "MyNamespace" {
		t.Errorf("Expected namespace MyNamespace, got %s", config.Namespace)
	}
	if config.OroVersion != "6.1" {
		t.Errorf("Expected OroVersion 6.1, got %s", config.OroVersion)
	}
	if len(config.Domains) != 1 || config.Domains[0].Host != "example.com" {
		t.Errorf("Unexpected domains: %+v", config.Domains)
	}
	if !config.Services.Mailpit {
		t.Errorf("Expected mailpit to be true")
	}

	if len(config.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(config.Commands))
	}
	if config.Commands[0].Name != "otr" {
		t.Errorf("Expected command name otr, got %s", config.Commands[0].Name)
	}
	if config.Commands[0].Command != "php bin/console oro:test:run" {
		t.Errorf("Expected command 'php bin/console oro:test:run', got %s", config.Commands[0].Command)
	}
	if config.Commands[0].Description != "Runs the Shippy Pro tests suite" {
		t.Errorf("Expected description 'Runs the Shippy Pro tests suite', got %s", config.Commands[0].Description)
	}
	if config.Commands[0].Service != "test-app" {
		t.Errorf("Expected service 'test-app', got %s", config.Commands[0].Service)
	}
}

func TestParseConfigProject(t *testing.T) {
	yamlData := `
type: project
oro_version: "6.1"
domains:
  - host: example.com
`
	config, err := ParseConfig([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if config.Type != InstallTypeProject {
		t.Errorf("Expected type project, got %s", config.Type)
	}
	if err := config.Validate(); err != nil {
		t.Errorf("project config without namespace should validate, got: %v", err)
	}
}

func TestParseConfigWithComposerRepositories(t *testing.T) {
	yamlData := `
namespace: MyNamespace
oro_version: "6.1"
domains:
  - host: example.com
composer:
  repositories:
    - type: vcs
      url: https://github.com/private/repo.git
    - type: composer
      url: https://repo.packagist.com/my-org/
`
	config, err := ParseConfig([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if len(config.Composer.Repositories) != 2 {
		t.Fatalf("Expected 2 repositories, got %d", len(config.Composer.Repositories))
	}

	repo0 := config.Composer.Repositories[0]
	if repo0["type"] != "vcs" {
		t.Errorf("Expected repo[0].type 'vcs', got %v", repo0["type"])
	}
	if repo0["url"] != "https://github.com/private/repo.git" {
		t.Errorf("Expected repo[0].url 'https://github.com/private/repo.git', got %v", repo0["url"])
	}

	repo1 := config.Composer.Repositories[1]
	if repo1["type"] != "composer" {
		t.Errorf("Expected repo[1].type 'composer', got %v", repo1["type"])
	}
}

func TestParseConfigWithDepends(t *testing.T) {
	yamlData := `
namespace: MyNamespace
oro_version: "6.1"
domains:
  - host: example.com
commands:
  - name: "build"
    command: "npm run build"
  - name: "test"
    command: "npm test"
    depends: ["build"]
`
	config, err := ParseConfig([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if len(config.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(config.Commands))
	}

	var testCmd *CommandConfig
	for i := range config.Commands {
		if config.Commands[i].Name == "test" {
			testCmd = &config.Commands[i]
		}
	}

	if testCmd == nil {
		t.Fatal("Command 'test' not found")
	}

	if len(testCmd.Depends) != 1 || testCmd.Depends[0] != "build" {
		t.Errorf("Expected depends ['build'], got %v", testCmd.Depends)
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "saveconfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, ".orobox.yaml")

	conf := &OroConfig{
		Namespace:  "MyNamespace",
		OroVersion: "6.1",
		Domains: []DomainConfig{
			{Host: "example.com"},
		},
		Services: ServicesConfig{
			Mailpit: true,
		},
	}

	err = SaveConfig(configPath, conf)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read saved config failed: %v", err)
	}

	content := string(data)
	if strings.Contains(content, "php:") {
		t.Errorf("Expected php section to be omitted from YAML if empty, but found it in:\n%s", content)
	}
}

func TestGetNamespace(t *testing.T) {
	viper.Reset()
	if GetNamespace() != "CustomBundle" {
		t.Errorf("Expected default namespace CustomBundle, got %s", GetNamespace())
	}

	viper.Set("namespace", "Override")
	if GetNamespace() != "Override" {
		t.Errorf("Expected overridden namespace Override, got %s", GetNamespace())
	}
}

func TestGetFirstDomainHost(t *testing.T) {
	viper.Reset()
	if GetFirstDomainHost() != "oro.demo" {
		t.Errorf("Expected default host oro.demo, got %s", GetFirstDomainHost())
	}

	domains := []DomainConfig{
		{Host: "test.domain"},
		{Host: "other.domain"},
	}
	viper.Set("domains", domains)

	if GetFirstDomainHost() != "test.domain" {
		t.Errorf("Expected host test.domain, got %s", GetFirstDomainHost())
	}
}

func TestFindPhpClass(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "findphpclass")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	className := "MyNamespace\\MyClass"
	fileName := "MyClass.php"

	err = os.WriteFile(fileName, []byte("<?php class MyClass {}"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	shortName, namespace, foundPath, found := FindPhpClass(".", className)
	if !found {
		t.Errorf("Expected to find PHP class %s", className)
	}
	if shortName != "MyClass" {
		t.Errorf("Expected short name MyClass, got %s", shortName)
	}
	if namespace != "MyNamespace" {
		t.Errorf("Expected namespace MyNamespace, got %s", namespace)
	}
	if foundPath != "MyClass.php" {
		t.Errorf("Expected foundPath MyClass.php, got %s", foundPath)
	}

	_, _, _, found = FindPhpClass(".", "NonExistent")
	if found {
		t.Errorf("Expected not to find NonExistent class")
	}
}

func TestGetInternalDir(t *testing.T) {
	t.Run("CI mode", func(t *testing.T) {
		os.Setenv("CI", "1")
		defer os.Unsetenv("CI")
		if GetInternalDir() != ".orobox" {
			t.Errorf("Expected internal directory .orobox in CI mode, got %s", GetInternalDir())
		}
	})

	t.Run("Local config mode", func(t *testing.T) {
		os.Setenv("OROBOX_LOCAL_CONFIG", "1")
		defer os.Unsetenv("OROBOX_LOCAL_CONFIG")
		if GetInternalDir() != ".orobox" {
			t.Errorf("Expected internal directory .orobox in local config mode, got %s", GetInternalDir())
		}
	})

	t.Run("Standard mode", func(t *testing.T) {
		os.Unsetenv("CI")
		os.Unsetenv("OROBOX_LOCAL_CONFIG")
		dir := GetInternalDir()
		if dir == ".orobox" {
			t.Errorf("Expected user config directory in standard mode, got %s", dir)
		}

		configDir, _ := os.UserConfigDir()
		projectName := GetProjectName()
		expected := filepath.Join(configDir, "orobox", projectName)
		if dir != expected {
			t.Errorf("Expected %s, got %s", expected, dir)
		}
	})
}

func TestParseConfigComposerSSHAgent(t *testing.T) {
	const base = `
type: project
oro_version: "6.1"
domains:
  - host: example.com
`

	t.Run("unset means auto-detect", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if conf.Composer.SSHAgent != nil {
			t.Errorf("expected ssh_agent to be nil when absent, got %v", *conf.Composer.SSHAgent)
		}
	})

	t.Run("true", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base + "composer:\n  ssh_agent: true\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if conf.Composer.SSHAgent == nil || !*conf.Composer.SSHAgent {
			t.Errorf("expected ssh_agent true, got %v", conf.Composer.SSHAgent)
		}
	})

	t.Run("false", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base + "composer:\n  ssh_agent: false\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if conf.Composer.SSHAgent == nil || *conf.Composer.SSHAgent {
			t.Errorf("expected ssh_agent false, got %v", conf.Composer.SSHAgent)
		}
	})
}

// An unset ssh_agent must not appear in a generated config file: a written `ssh_agent: false`
// would silently pin forwarding off for every future checkout of that repository.
func TestSaveConfigOmitsUnsetSSHAgent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".orobox.yaml")

	conf := &OroConfig{
		Type:       InstallTypeProject,
		OroVersion: "6.1",
		Domains:    []DomainConfig{{Host: "example.com"}},
	}
	if err := SaveConfig(configPath, conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read saved config failed: %v", err)
	}
	if strings.Contains(string(data), "ssh_agent") {
		t.Errorf("expected ssh_agent to be omitted when unset, got:\n%s", data)
	}
}

// A config file that says nothing about QA must survive a load/save round-trip with every tool
// still enabled. IsQaToolEnabled treats an unset key as enabled, so a written `phpstan: false`
// does not just record the current state — it silently disables the tool for good. `orobox
// deploy-init` rewrites the whole file, so this is the difference between `orobox qa-init`
// installing the tool set and reporting "No QA tools are enabled in configuration".
func TestSaveConfigKeepsUnsetQaToolsEnabled(t *testing.T) {
	viper.Reset()
	viper.SetConfigType("yaml")
	source := "type: project\noro_version: \"6.1\"\ntest:\n  use_tmpfs: true\n  tmpfs_size: 1g\n"
	if err := viper.ReadConfig(strings.NewReader(source)); err != nil {
		t.Fatal(err)
	}

	var conf OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	if err := SaveConfig(configPath, &conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read saved config failed: %v", err)
	}
	if strings.Contains(string(data), "phpstan") {
		t.Errorf("expected the qa section to be omitted when unset, got:\n%s", data)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	for _, tool := range []string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint"} {
		if !IsQaToolEnabled(tool) {
			t.Errorf("%s is disabled after a round-trip that never configured it", tool)
		}
	}
}

// An explicitly disabled tool is a decision, and a round-trip must preserve it.
func TestSaveConfigPreservesExplicitQaTools(t *testing.T) {
	viper.Reset()
	viper.SetConfigType("yaml")
	source := "type: project\ntest:\n  qa:\n    phpstan: false\n    rector: true\n"
	if err := viper.ReadConfig(strings.NewReader(source)); err != nil {
		t.Fatal(err)
	}

	var conf OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	if err := SaveConfig(configPath, &conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if IsQaToolEnabled("phpstan") {
		t.Error("an explicit phpstan: false was lost by the round-trip")
	}
	if !IsQaToolEnabled("rector") {
		t.Error("an explicit rector: true was lost by the round-trip")
	}
	if !IsQaToolEnabled("eslint") {
		t.Error("eslint was never configured and must stay enabled")
	}
}

func TestQaSharedPackagesIsACopy(t *testing.T) {
	first := QaSharedPackages()
	if len(first) == 0 {
		t.Fatal("no shared packages")
	}
	first[0] = "mutated"

	if second := QaSharedPackages(); second[0] == "mutated" {
		t.Error("QaSharedPackages hands out the list the constraints are built from")
	}

	// Every shared package needs a constraint for the case where the application does not ship
	// it: without one, bamarni resolves that package against the newest Symfony line.
	constraints := strings.Join(GetQaSymfonyConstraints("6.1"), " ")
	for _, name := range QaSharedPackages() {
		if name == "psr/log" {
			// Only capped on Symfony 5.4; see GetQaSymfonyConstraints.
			continue
		}
		if !strings.Contains(constraints, name+":") {
			t.Errorf("%s is shared but unconstrained: %s", name, constraints)
		}
	}
}

// imageConfigBase is the smallest config Validate accepts, so each image test only has to vary
// the block it is about.
const imageConfigBase = `
type: project
oro_version: "6.1"
domains:
  - host: example.com
`

func TestValidateImageEntries(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantErr string // substring of the expected error; "" means the config must be accepted
	}{
		{"empty apk entry", "image:\n  apk: [\"\"]\n", "image.apk"},
		{"shell metacharacters in php_extensions", "image:\n  php_extensions: [\"redis; rm -rf /\"]\n", "image.php_extensions"},
		{"space inside an npm entry", "image:\n  npm: [\"left pad\"]\n", "image.npm"},
		{"scoped npm package", "image:\n  npm: [\"@playwright/test\"]\n", ""},
		{"apk with a pinned version", "image:\n  apk: [\"php84-pecl-redis=6.1.0-r0\"]\n", ""},
		{"run is free-form", "image:\n  run: [\"a && b | c\"]\n", ""},
		{"empty run entry", "image:\n  run: [\"\"]\n", "image.run"},
		{"whitespace-only run entry", "image:\n  run: [\"   \"]\n", "image.run"},
		// A line break would start a new Dockerfile instruction once spliced after "RUN ".
		{"run entry with a newline", "image:\n  run: [\"set -e\\nFROM alpine\\n\"]\n", "image.run"},
		{"run entry with a carriage return", "image:\n  run: [\"a\\rFROM alpine\"]\n", "image.run"},
		{"run entry ending in a backslash", "image:\n  run: [\"apk add git \\\\\"]\n", "image.run"},
		{"run entry ending in a backslash and spaces", "image:\n  run: [\"apk add git \\\\  \"]\n", "image.run"},
		{"run entry joined with &&", "image:\n  run: [\"a && b\"]\n", ""},
		{"no image block", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := ParseConfig([]byte(imageConfigBase + tt.image))
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			err = conf.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected the config to be accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected the error to name %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateImageDockerfilePath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr string
	}{
		{"/abs/Dockerfile", "relative"},
		{"../out/Dockerfile", "inside the project"},
		{"..", "inside the project"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			conf, err := ParseConfig([]byte(imageConfigBase + "image:\n  dockerfile: " + tt.path + "\n"))
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			err = conf.Validate()
			if err == nil {
				t.Fatalf("expected %q to be rejected", tt.path)
			}
			if !strings.Contains(err.Error(), "image.dockerfile") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected an image.dockerfile error mentioning %q, got %v", tt.wantErr, err)
			}
		})
	}

	conf, err := ParseConfig([]byte(imageConfigBase + "image:\n  dockerfile: ./docker/Dockerfile\n"))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if err := conf.Validate(); err != nil {
		t.Errorf("expected a project-relative path to be accepted, got %v", err)
	}
}

func TestValidateBothDockerfileKeys(t *testing.T) {
	conf, err := ParseConfig([]byte(imageConfigBase + "dockerfile: a/Dockerfile\nimage:\n  dockerfile: b/Dockerfile\n"))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	err = conf.Validate()
	if err == nil {
		t.Fatal("expected setting both dockerfile keys to be rejected")
	}
	if !strings.Contains(err.Error(), "'dockerfile'") || !strings.Contains(err.Error(), "image.dockerfile") {
		t.Errorf("expected the error to name both keys, got %v", err)
	}
}

func TestImageSettingsFoldsDeprecatedKey(t *testing.T) {
	t.Run("top-level key only", func(t *testing.T) {
		conf, err := ParseConfig([]byte(imageConfigBase + "dockerfile: docker/Dockerfile\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if got := conf.ImageSettings().Dockerfile; got != "docker/Dockerfile" {
			t.Errorf("ImageSettings().Dockerfile = %q, want docker/Dockerfile", got)
		}
	})

	t.Run("image key wins and keeps its siblings", func(t *testing.T) {
		conf, err := ParseConfig([]byte(imageConfigBase + "image:\n  dockerfile: b/Dockerfile\n  apk: [git]\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		got := conf.ImageSettings()
		if got.Dockerfile != "b/Dockerfile" || len(got.Apk) != 1 || got.Apk[0] != "git" {
			t.Errorf("unexpected ImageSettings(): %+v", got)
		}
	})

	// GetImageConfig normalizes the path for the dev stack; the pipeline reads this method, so
	// both must agree on what the path is.
	t.Run("dockerfile path is normalized", func(t *testing.T) {
		for _, raw := range []string{"dockerfile", "image"} {
			yaml := imageConfigBase + "dockerfile: \"  ./docker/Dockerfile  \"\n"
			if raw == "image" {
				yaml = imageConfigBase + "image:\n  dockerfile: \"  ./docker/Dockerfile  \"\n"
			}
			conf, err := ParseConfig([]byte(yaml))
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			if got := conf.ImageSettings().Dockerfile; got != "docker/Dockerfile" {
				t.Errorf("%s form: ImageSettings().Dockerfile = %q, want docker/Dockerfile", raw, got)
			}
		}
	})

	t.Run("whitespace-only dockerfile becomes empty", func(t *testing.T) {
		conf := &OroConfig{Image: &ImageConfig{Dockerfile: "   "}}
		if got := conf.ImageSettings().Dockerfile; got != "" {
			t.Errorf("ImageSettings().Dockerfile = %q, want empty", got)
		}
	})

	t.Run("no image configuration", func(t *testing.T) {
		conf, err := ParseConfig([]byte(imageConfigBase))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if !conf.ImageSettings().IsEmpty() {
			t.Errorf("expected empty settings, got %+v", conf.ImageSettings())
		}
	})
}

// A config written by an older Orobox keeps working, and the next rewrite moves the path under
// image: so the deprecated key does not outlive the first `orobox deploy-init`.
func TestSaveConfigMigratesDockerfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	conf := &OroConfig{Type: InstallTypeProject, OroVersion: "6.1", Dockerfile: "docker/Dockerfile"}
	if err := SaveConfig(configPath, conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "image:\n    dockerfile: docker/Dockerfile") {
		t.Errorf("expected the path under image:, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "dockerfile:") {
			t.Errorf("expected no top-level dockerfile line, got:\n%s", out)
		}
	}
	if conf.Dockerfile != "docker/Dockerfile" {
		t.Errorf("SaveConfig must not mutate the caller's config, Dockerfile is now %q", conf.Dockerfile)
	}
}

func TestHasCustomLayer(t *testing.T) {
	tests := []struct {
		name string
		set  map[string]any
		want bool
	}{
		{"nothing configured", nil, false},
		{"apk", map[string]any{"image.apk": []string{"x"}}, true},
		{"run", map[string]any{"image.run": []string{"echo hi"}}, true},
		{"image.dockerfile", map[string]any{"image.dockerfile": "docker/Dockerfile"}, true},
		{"deprecated top-level dockerfile", map[string]any{"dockerfile": "docker/Dockerfile"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			for k, v := range tt.set {
				viper.Set(k, v)
			}
			if got := HasCustomLayer(); got != tt.want {
				t.Errorf("HasCustomLayer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeprecatedDockerfileKeyUsed(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	if DeprecatedDockerfileKeyUsed() {
		t.Error("expected false when nothing is set")
	}
	viper.Set("image.dockerfile", "docker/Dockerfile")
	if DeprecatedDockerfileKeyUsed() {
		t.Error("image.dockerfile is the new key, not the deprecated one")
	}
	viper.Set("dockerfile", "docker/Dockerfile")
	if !DeprecatedDockerfileKeyUsed() {
		t.Error("expected true when the top-level key is set")
	}
}

// validateYAML parses and validates imageConfigBase plus extra, returning the first error either
// step reports, so a test can assert on a rejection whichever layer produces it.
func validateYAML(t *testing.T, extra string) error {
	t.Helper()
	conf, err := ParseConfig([]byte(imageConfigBase + extra))
	if err != nil {
		return err
	}
	return conf.Validate()
}

// The legacy nginx_*_port keys are documented and read by GetNginxPorts, so ParseConfig must not
// refuse them as unknown fields.
func TestLegacyNginxPortKeys(t *testing.T) {
	tests := []struct {
		name    string
		extra   string
		wantErr string
	}{
		{"http accepted", "nginx_http_port: 8090\n", ""},
		{"https accepted", "nginx_https_port: 8453\n", ""},
		{"zero is not a port", "nginx_http_port: 0\n", "nginx_http_port"},
		{"too large", "nginx_https_port: 70000\n", "nginx_https_port"},
		{"negative", "nginx_http_port: -1\n", "nginx_http_port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateYAML(t, tt.extra)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the config to be accepted, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected an error naming %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// A null port value decodes to 0 in a map[string]int, which Validate would accept and GetPorts
// would read as unset: the user asked for something and silently got the default.
func TestNullPortIsRejected(t *testing.T) {
	err := validateYAML(t, "ports:\n  db: ~\n")
	if err == nil {
		t.Fatal("expected `ports.db: ~` to be rejected")
	}
	for _, want := range []string{"ports.db", "0 to not publish"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if err := validateYAML(t, "ports:\n  db: 0\n  redis: 6380\n"); err != nil {
		t.Errorf("explicit numbers must stay accepted, got %v", err)
	}
}

// A folded scalar (`>`) ends in a newline the user never typed; it is stripped rather than
// rejected, and every reader of image.run sees the stripped value.
func TestImageRunFoldedScalar(t *testing.T) {
	yaml := "image:\n  run:\n    - >\n      apk add git\n      && echo done\n"
	conf, err := ParseConfig([]byte(imageConfigBase + yaml))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if err := conf.Validate(); err != nil {
		t.Fatalf("expected a folded scalar to be accepted, got %v", err)
	}
	want := "apk add git && echo done"
	if got := conf.ImageSettings().Run; len(got) != 1 || got[0] != want {
		t.Errorf("ImageSettings().Run = %q, want [%q]", got, want)
	}
	if conf.Image.Run[0] != want+"\n" {
		t.Errorf("ImageSettings must not mutate the config, Run[0] is now %q", conf.Image.Run[0])
	}

	viper.Reset()
	defer viper.Reset()
	viper.Set("image.run", []string{want + "\r\n"})
	if got := GetImageConfig().Run; len(got) != 1 || got[0] != want {
		t.Errorf("GetImageConfig().Run = %q, want [%q]", got, want)
	}

	// Only the trailing terminators go: a line break inside the entry still starts a new
	// Dockerfile instruction.
	if err := validateYAML(t, "image:\n  run:\n    - |\n      set -e\n      FROM alpine\n"); err == nil || !strings.Contains(err.Error(), "image.run") {
		t.Errorf("expected an internal newline to be rejected, got %v", err)
	}
}

func TestImageRunRejectsHeredoc(t *testing.T) {
	tests := []struct {
		entry   string
		wantErr bool
	}{
		{"cat <<EOF > /etc/x", true},
		{"cat <<-EOF > /etc/x", true},
		{"cat << 'EOF' > /etc/x", true},
		{`cat <<"EOF" > /etc/x`, true},
		{"cat <<_END", true},
		// A backslash-quoted delimiter is a heredoc to BuildKit too.
		{`cat <<\EOF > /x`, true},
		{`cat <<-\EOF > /x`, true},
		// A here-string is single-line and not a Dockerfile heredoc.
		{`cat <<< "hello"`, false},
		{"tr a b <<<word", false},
		{"echo $((1<<4))", false},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			err := validateYAML(t, "image:\n  run: ["+strconv.Quote(tt.entry)+"]\n")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected %q to be accepted, got %v", tt.entry, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %q to be rejected", tt.entry)
			}
			if !strings.Contains(err.Error(), "image.run") || !strings.Contains(err.Error(), "image.dockerfile") {
				t.Errorf("expected the error to name image.run and point at image.dockerfile, got %v", err)
			}
		})
	}
}

// An entry starting with "-" would be read by apk, docker-php-ext-install or npm as an option.
func TestImageListEntriesRejectLeadingOption(t *testing.T) {
	for _, key := range []string{"apk", "php_extensions", "npm"} {
		for _, entry := range []string{"--allow-untrusted", "-g", ".hidden", "=1.0"} {
			t.Run(key+" "+entry, func(t *testing.T) {
				err := validateYAML(t, "image:\n  "+key+": ["+strconv.Quote(entry)+"]\n")
				if err == nil {
					t.Fatalf("expected %q in image.%s to be rejected", entry, key)
				}
				for _, want := range []string{"image." + key, "move it to image.run"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q should mention %q", err, want)
					}
				}
			})
		}
	}
	// Every rejected entry gets the hint, not only the leading-dash case.
	if err := validateYAML(t, "image:\n  apk: [\"git; rm -rf /\"]\n"); err == nil || !strings.Contains(err.Error(), "move it to image.run") {
		t.Errorf("expected the image.run hint for a shell metacharacter, got %v", err)
	}
	for _, ok := range []string{"@playwright/test", "9base", "php84-pecl-redis=6.1.0-r0"} {
		if err := validateYAML(t, "image:\n  npm: ["+strconv.Quote(ok)+"]\n"); err != nil {
			t.Errorf("expected %q to be accepted, got %v", ok, err)
		}
	}
}

// These paths end up in a compose short-syntax volume (`src:dst`) or a build context, where a
// colon, quote, backslash, variable or control character changes what Docker reads.
func TestProjectPathKeysRejectComposeMetacharacters(t *testing.T) {
	keys := []struct {
		key  string
		yaml func(string) string
	}{
		{"image.dockerfile", func(v string) string { return "image:\n  dockerfile: " + v + "\n" }},
		{"dockerfile", func(v string) string { return "dockerfile: " + v + "\n" }},
		{"php_ini", func(v string) string { return "php_ini: " + v + "\n" }},
	}
	for _, k := range keys {
		for _, bad := range []string{"conf/a:b.ini", `conf/a\"b.ini`, `conf\\a.ini`, "conf/$HOME.ini", `conf/a\tb.ini`, `conf/a\x01b.ini`} {
			t.Run(k.key+" "+bad, func(t *testing.T) {
				err := validateYAML(t, k.yaml(`"`+bad+`"`))
				if err == nil {
					t.Fatalf("expected %s to be rejected for %s", bad, k.key)
				}
				if !strings.Contains(err.Error(), "'"+k.key+"'") {
					t.Errorf("expected the error to name '%s', got %v", k.key, err)
				}
			})
		}
	}
}

func TestValidateNamesTheDomainIndex(t *testing.T) {
	conf := OroConfig{Type: InstallTypeProject, OroVersion: "6.1", Domains: []DomainConfig{{Host: "a.test"}, {Host: ""}}}
	err := conf.Validate()
	if err == nil || !strings.HasSuffix(err.Error(), "index 1") {
		t.Errorf("expected the error to end with \"index 1\", got %q", err)
	}
}
