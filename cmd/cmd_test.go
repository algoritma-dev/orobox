package cmd

import (
	"bytes"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/spf13/viper"
	"strings"
	"testing"
	"testing/fstest"
)

func init() {
	docker.Templates = fstest.MapFS{
		"templates/docker/Dockerfile":           &fstest.MapFile{Data: []byte("FROM php:{{.PHPVersion}}-fpm")},
		"templates/docker/.env":                 &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}")},
		"templates/docker/nginx.conf":           &fstest.MapFile{Data: []byte("server { listen 80; }")},
		"templates/docker/init-db.sql":          &fstest.MapFile{Data: []byte("CREATE DATABASE oro;")},
		"templates/docker/docker-entrypoint.sh": &fstest.MapFile{Data: []byte("#!/bin/bash")},
		"templates/docker/docker-compose.yml":   &fstest.MapFile{Data: []byte("version: '3'")},
	}
}

func TestUpCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var calls [][]string
	docker.RunComposeCommand = func(args ...string) error {
		calls = append(calls, args)
		return nil
	}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"up"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(calls) == 3 {
		if calls[0][0] != "build" {
			t.Errorf("Expected first call to be build, got %v", calls[0])
		}
		calls = calls[1:]
	}

	if len(calls) != 2 {
		t.Errorf("Expected 2 more calls to RunComposeCommand, got %d", len(calls))
		return
	}

	if len(calls[0]) < 3 || calls[0][0] != "run" || calls[0][2] != "restore" {
		t.Errorf("Expected call to be run --rm restore, got %v", calls[0])
	}
	if len(calls[1]) < 3 || calls[1][0] != "up" || calls[1][2] != "application" {
		t.Errorf("Expected call to be up -d application, got %v", calls[1])
	}
}

func TestDownCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommand = func(args ...string) error {
		capturedArgs = args
		return nil
	}

	rootCmd.SetArgs([]string{"down"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if capturedArgs[0] != "down" {
		t.Errorf("Expected down command, got %v", capturedArgs)
	}
}

func TestCleanCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommand = func(args ...string) error {
		capturedArgs = args
		return nil
	}

	rootCmd.SetArgs([]string{"clean"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	// clean calls down -v --remove-orphans
	if capturedArgs[0] != "down" || !contains(capturedArgs, "-v") {
		t.Errorf("Expected down -v, got %v", capturedArgs)
	}
}

func TestTestCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommand = func(args ...string) error {
		capturedArgs = args
		return nil
	}

	rootCmd.SetArgs([]string{"test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if capturedArgs[0] != "exec" || !contains(capturedArgs, "application") || !contains(capturedArgs, "APP_ENV=test") || !contains(capturedArgs, "ORO_ENV=test") || !contains(capturedArgs, "ORO_DB_NAME=oro_db_test") {
		t.Errorf("Expected exec application with test environment, got %v", capturedArgs)
	}
}

func TestShellCommand(t *testing.T) {
	oldRun := runInteractiveShell
	defer func() { runInteractiveShell = oldRun }()

	var capturedService string
	runInteractiveShell = func(service string) {
		capturedService = service
	}

	rootCmd.SetArgs([]string{"shell"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if capturedService != "application" {
		t.Errorf("Expected application service, got %v", capturedService)
	}
}

func TestConsoleCommand(t *testing.T) {
	oldRun := runConsole
	defer func() { runConsole = oldRun }()

	var capturedArgs []string
	runConsole = func(args []string) {
		capturedArgs = args
	}

	expectedArgs := []string{"cache:clear", "--no-warmup"}
	rootCmd.SetArgs(append([]string{"console"}, expectedArgs...))
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(capturedArgs) != len(expectedArgs) {
		t.Fatalf("Expected %v args, got %v", len(expectedArgs), len(capturedArgs))
	}

	for i := range expectedArgs {
		if capturedArgs[i] != expectedArgs[i] {
			t.Errorf("Expected arg %d to be %s, got %s", i, expectedArgs[i], capturedArgs[i])
		}
	}
}

func TestLogsCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommand = func(args ...string) error {
		capturedArgs = args
		return nil
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			"nginx logs",
			[]string{"logs", "--nginx"},
			[]string{"logs", "-f", "web"},
		},
		{
			"php logs",
			[]string{"logs", "--php"},
			[]string{"logs", "-f", "php-fpm-app"},
		},
		{
			"app logs",
			[]string{"logs", "--app"},
			[]string{"logs", "-f", "application", "php-fpm-app"},
		},
		{
			"multiple logs",
			[]string{"logs", "--nginx", "--php"},
			[]string{"logs", "-f", "web", "php-fpm-app"},
		},
		{
			"consumer logs",
			[]string{"logs", "--consumer"},
			[]string{"logs", "-f", "consumer"},
		},
		{
			"cron logs",
			[]string{"logs", "--cron"},
			[]string{"logs", "-f", "cron"},
		},
		{
			"ws logs",
			[]string{"logs", "--ws"},
			[]string{"logs", "-f", "ws"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedArgs = nil
			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("rootCmd.Execute() failed: %v", err)
			}

			if len(capturedArgs) != len(tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, capturedArgs)
				return
			}

			for i := range tt.expected {
				if capturedArgs[i] != tt.expected[i] {
					t.Errorf("Expected %v, got %v", tt.expected, capturedArgs)
					break
				}
			}
		})
	}
}

func TestTestCommandBundle(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommand = func(args ...string) error {
		capturedArgs = args
		return nil
	}

	viper.Set("type", "bundle")
	viper.Set("namespace", "MyTestBundle")
	defer func() {
		viper.Set("type", nil)
		viper.Set("namespace", nil)
	}()

	rootCmd.SetArgs([]string{"test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if !contains(capturedArgs, "simple-phpunit") || !contains(capturedArgs, "--configuration=src/MyTestBundle") {
		t.Errorf("Expected simple-phpunit with configuration, got %v", capturedArgs)
	}
	if !contains(capturedArgs, "APP_ENV=test") || !contains(capturedArgs, "ORO_ENV=test") || !contains(capturedArgs, "ORO_DB_NAME=oro_db_test") {
		t.Errorf("Expected test environment, got %v", capturedArgs)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.Contains(s, item) {
			return true
		}
	}
	return false
}
