package cmd

import (
	"bytes"
	"github.com/algoritma-dev/orobox/internal/docker"
	"strings"
	"testing"
)

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

	if len(calls) != 2 {
		t.Errorf("Expected 2 calls to RunComposeCommand, got %d", len(calls))
	}

	if calls[0][1] != "install" {
		t.Errorf("Expected first call to be install, got %v", calls[0])
	}
	if calls[1][1] != "application" {
		t.Errorf("Expected second call to be application, got %v", calls[1])
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

	if capturedArgs[0] != "exec" || !contains(capturedArgs, "application") {
		t.Errorf("Expected exec application, got %v", capturedArgs)
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

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.Contains(s, item) {
			return true
		}
	}
	return false
}
