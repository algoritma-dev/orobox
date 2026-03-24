package cmd

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/spf13/viper"
)

func init() {
	docker.Templates = fstest.MapFS{
		"templates/docker/Dockerfile":               &fstest.MapFile{Data: []byte("FROM php:{{.PHPVersion}}-fpm")},
		"templates/docker/.env":                     &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}\n")},
		"templates/docker/.env.test":                &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}\n")},
		"templates/docker/nginx.conf":               &fstest.MapFile{Data: []byte("server { listen 80; }")},
		"templates/docker/init-db.sql":              &fstest.MapFile{Data: []byte("CREATE DATABASE oro;")},
		"templates/docker/docker-entrypoint.sh":     &fstest.MapFile{Data: []byte("#!/bin/bash")},
		"templates/docker/docker-compose.yml":       &fstest.MapFile{Data: []byte("version: '3'")},
		"templates/docker/docker-compose.setup.yml": &fstest.MapFile{Data: []byte("version: '3'")},
		"templates/docker/docker-compose.test.yml":  &fstest.MapFile{Data: []byte("version: '3'")},
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

	if len(calls) > 0 && calls[0][0] == "build" {
		calls = calls[1:]
	}

	if len(calls) != 2 {
		t.Errorf("Expected 2 more calls to RunComposeCommand (restore, up), got %d: %v", len(calls), calls)
		return
	}

	if len(calls[0]) < 3 || calls[0][0] != "run" || calls[0][2] != "restore" {
		t.Errorf("Expected call to be run --rm restore, got %v", calls[0])
	}
	if len(calls[1]) < 3 || calls[1][0] != "up" || !contains(calls[1], "application") {
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

	var calls [][]string
	docker.RunComposeCommand = func(args ...string) error {
		calls = append(calls, args)
		return nil
	}

	rootCmd.SetArgs([]string{"test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(calls) != 3 {
		t.Errorf("Expected 3 calls to RunComposeCommand (up, restore, exec), got %d: %v", len(calls), calls)
		return
	}

	// 1st call: up -d application_test
	if calls[0][0] != "up" || !contains(calls[0], "application_test") {
		t.Errorf("Expected first call to be up -d application_test, got %v", calls[0])
	}

	// 2nd call: exec restore
	if calls[1][0] != "exec" || !contains(calls[1], "application_test") || !contains(calls[1], "restore") {
		t.Errorf("Expected second call to be exec application_test restore, got %v", calls[1])
	}

	// 3rd call: actual test execution
	lastCall := calls[2]
	if lastCall[0] != "exec" || !contains(lastCall, "application_test") || !contains(lastCall, "APP_ENV=test") || !contains(lastCall, "ORO_ENV=test") || !contains(lastCall, "ORO_DB_NAME=oro_db_test") {
		t.Errorf("Expected exec application_test with test environment, got %v", lastCall)
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

	var calls [][]string
	docker.RunComposeCommand = func(args ...string) error {
		calls = append(calls, args)
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

	if len(calls) != 3 {
		t.Errorf("Expected 3 calls to RunComposeCommand, got %d: %v", len(calls), calls)
		return
	}

	lastCall := calls[2]
	if !contains(lastCall, "simple-phpunit") || !contains(lastCall, "--configuration=src/MyTestBundle") {
		t.Errorf("Expected simple-phpunit with configuration, got %v", lastCall)
	}
	if !contains(lastCall, "APP_ENV=test") || !contains(lastCall, "ORO_ENV=test") || !contains(lastCall, "ORO_DB_NAME=oro_db_test") {
		t.Errorf("Expected test environment, got %v", lastCall)
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
