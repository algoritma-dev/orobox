package config

import (
	"os"
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
    version: "8.4"
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
	if config.Services.Php.Version != "8.4" {
		t.Errorf("Expected php version 8.4, got %s", config.Services.Php.Version)
	}
	if !config.Services.Php.Xdebug {
		t.Errorf("Expected xdebug to be true")
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
