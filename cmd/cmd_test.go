package cmd

import (
	"bytes"
	"fmt"
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
	oldRunSilently := docker.RunComposeCommandSilently
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
	}()

	var calls [][]string
	mockRun := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	mockRunSilently := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommand = mockRun
	docker.RunComposeCommandSilently = mockRunSilently

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"up"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	for len(calls) > 0 && (calls[0][0] == "pull" || calls[0][0] == "build") {
		calls = calls[1:]
	}

	if len(calls) != 1 {
		t.Errorf("Expected 1 more call to RunComposeCommand (up), got %d: %v", len(calls), calls)
		return
	}

	if len(calls[0]) < 2 || calls[0][0] != "up" || !contains(calls[0], "-d") {
		t.Errorf("Expected call to be up -d, got %v", calls[0])
	}
}

func TestDownCommand(t *testing.T) {
	oldRun := docker.RunComposeCommandSilently
	defer func() { docker.RunComposeCommandSilently = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommandSilently = func(_ string, args ...string) error {
		capturedArgs = args
		return nil
	}

	rootCmd.SetArgs([]string{"down"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(capturedArgs) == 0 {
		t.Fatal("Expected call to RunComposeCommandSilently, got 0")
	}

	if capturedArgs[0] != "down" {
		t.Errorf("Expected down command, got %v", capturedArgs)
	}
}

func TestCleanCommand(t *testing.T) {
	oldRun := docker.RunComposeCommandSilently
	defer func() { docker.RunComposeCommandSilently = oldRun }()

	var capturedArgs []string
	docker.RunComposeCommandSilently = func(_ string, args ...string) error {
		capturedArgs = args
		return nil
	}

	rootCmd.SetArgs([]string{"clean"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(capturedArgs) == 0 {
		t.Fatal("Expected call to RunComposeCommandSilently, got 0")
	}

	// clean calls down -v --remove-orphans
	if capturedArgs[0] != "down" || !contains(capturedArgs, "-v") {
		t.Errorf("Expected down -v, got %v", capturedArgs)
	}
}

func TestTestCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
	}()

	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() { docker.RunComposeCommandWithOutput = oldRunWithOutput }()

	var calls [][]string
	mockRun := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	mockRunSilently := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommand = mockRun
	docker.RunComposeCommandSilently = mockRunSilently

	docker.RunComposeCommandWithOutput = func(_ ...string) ([]byte, error) {
		return []byte("OK"), nil
	}

	rootCmd.SetArgs([]string{"test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(calls) != 2 {
		t.Errorf("Expected 2 calls to RunComposeCommand (up, exec), got %d: %v", len(calls), calls)
		return
	}

	// 1st call: up -d application_test
	if calls[0][0] != "up" || !contains(calls[0], "application_test") {
		t.Errorf("Expected first call to be up -d application_test, got %v", calls[0])
	}

	// 2nd call: actual test execution
	lastCall := calls[1]
	if lastCall[0] != "exec" || !contains(lastCall, "application_test") {
		t.Errorf("Expected exec application_test, got %v", lastCall)
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
	docker.RunComposeCommand = func(_ string, args ...string) error {
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
	oldRunSilently := docker.RunComposeCommandSilently
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
	}()

	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() { docker.RunComposeCommandWithOutput = oldRunWithOutput }()

	var calls [][]string
	mockRun := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	mockRunSilently := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommand = mockRun
	docker.RunComposeCommandSilently = mockRunSilently

	docker.RunComposeCommandWithOutput = func(_ ...string) ([]byte, error) {
		return []byte("OK"), nil
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

	if len(calls) != 2 {
		t.Errorf("Expected 2 calls to RunComposeCommand, got %d: %v", len(calls), calls)
		return
	}

	lastCall := calls[1]
	if !contains(lastCall, "simple-phpunit") || !contains(lastCall, "--configuration=src/MyTestBundle") {
		t.Errorf("Expected simple-phpunit with configuration, got %v", lastCall)
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

func TestTestInitCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
	}()

	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() { docker.RunComposeCommandWithOutput = oldRunWithOutput }()

	var calls [][]string
	mockRun := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	mockRunSilently := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommand = mockRun
	docker.RunComposeCommandSilently = mockRunSilently

	// Simula ambiente NON inizializzato per evitare prompt
	docker.RunComposeCommandWithOutput = func(_ ...string) ([]byte, error) {
		return nil, fmt.Errorf("not initialized")
	}

	rootCmd.SetArgs([]string{"test-init"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	// Expected calls: up, drop (psql), create, cache clear, install
	if len(calls) < 5 {
		t.Errorf("Expected at least 5 calls to RunComposeCommand, got %d: %v", len(calls), calls)
		return
	}

	if calls[0][0] != "up" || !contains(calls[0], "application_test") {
		t.Errorf("Expected first call to be up, got %v", calls[0])
	}

	foundDrop := false
	foundCreate := false
	foundCacheClear := false
	foundInstall := false
	for _, call := range calls {
		if contains(call, "psql") && contains(call, "DROP DATABASE") {
			foundDrop = true
		}
		if contains(call, "doctrine:database:create") {
			foundCreate = true
		}
		if contains(call, "rm -rf var/cache/test") {
			foundCacheClear = true
		}
		if contains(call, "oro:install") {
			foundInstall = true
		}
	}

	if !foundDrop {
		t.Errorf("database drop command (psql) not found in calls")
	}
	if !foundCreate {
		t.Errorf("database create command not found in calls")
	}
	if !foundCacheClear {
		t.Errorf("cache clear command not found in calls")
	}
	if !foundInstall {
		t.Errorf("oro:install command not found in calls")
	}
}
