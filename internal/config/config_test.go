package config

import (
	"os"
	"path/filepath"
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
  php:
    xdebug: true
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
	if !config.Services.Php.Xdebug {
		t.Errorf("Expected xdebug to be true")
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
			Php: PhpConfig{
				Xdebug: false, // Should be omitted
			},
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
	if strings.Contains(content, "xdebug") {
		t.Errorf("Expected xdebug to be omitted from YAML, but found it in:\n%s", content)
	}
	if strings.Contains(content, "php:") {
		t.Errorf("Expected php section to be omitted from YAML if empty, but found it in:\n%s", content)
	}

	// Now try with Xdebug: true
	conf.Services.Php.Xdebug = true
	err = SaveConfig(configPath, conf)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read saved config failed: %v", err)
	}

	content = string(data)
	if !strings.Contains(content, "xdebug: true") {
		t.Errorf("Expected xdebug: true to be in YAML, but not found in:\n%s", content)
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

	shortName, namespace, found := FindPhpClass(className)
	if !found {
		t.Errorf("Expected to find PHP class %s", className)
	}
	if shortName != "MyClass" {
		t.Errorf("Expected short name MyClass, got %s", shortName)
	}
	if namespace != "MyNamespace" {
		t.Errorf("Expected namespace MyNamespace, got %s", namespace)
	}

	_, _, found = FindPhpClass("NonExistent")
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
