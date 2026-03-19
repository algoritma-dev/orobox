package cmd

import (
	"bytes"
	"github.com/algoritma-dev/orobox/internal/docker"
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

	if len(calls[0]) < 2 || calls[0][1] != "install" {
		t.Errorf("Expected call to be install, got %v", calls[0])
	}
	if len(calls[1]) < 2 || calls[1][1] != "application" {
		t.Errorf("Expected call to be application, got %v", calls[1])
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
