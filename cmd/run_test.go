package cmd

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestResolveDependencies(t *testing.T) {
	commands := []config.CommandConfig{
		{Name: "a", Depends: []string{"b", "c"}},
		{Name: "b", Depends: []string{"d"}},
		{Name: "c", Command: "echo c"},
		{Name: "d", Command: "echo d"},
	}

	var executionList []*config.CommandConfig
	visited := make(map[string]bool)
	path := make(map[string]bool)

	err := resolveDependencies("a", commands, &executionList, visited, path)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(executionList) != 4 {
		t.Fatalf("Expected 4 commands in execution list, got %d", len(executionList))
	}

	// Order should be: d, b, c, a
	expected := []string{"d", "b", "c", "a"}
	for i, cmd := range executionList {
		if cmd.Name != expected[i] {
			t.Errorf("At index %d, expected %s, got %s", i, expected[i], cmd.Name)
		}
	}
}

func TestResolveDependencies_Circular(t *testing.T) {
	commands := []config.CommandConfig{
		{Name: "a", Depends: []string{"b"}},
		{Name: "b", Depends: []string{"a"}},
	}

	var executionList []*config.CommandConfig
	visited := make(map[string]bool)
	path := make(map[string]bool)

	err := resolveDependencies("a", commands, &executionList, visited, path)
	if err == nil {
		t.Fatal("Expected error due to circular dependency, got nil")
	}

	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestResolveDependencies_Complex(t *testing.T) {
	commands := []config.CommandConfig{
		{Name: "all", Depends: []string{"test", "build"}},
		{Name: "test", Depends: []string{"build"}},
		{Name: "build", Command: "npm run build"},
	}

	var executionList []*config.CommandConfig
	visited := make(map[string]bool)
	path := make(map[string]bool)

	err := resolveDependencies("all", commands, &executionList, visited, path)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// build should be first, then test, then all
	// executionList should only contain build once if we use visited map
	if len(executionList) != 3 {
		t.Fatalf("Expected 3 commands in execution list, got %d", len(executionList))
	}

	expected := []string{"build", "test", "all"}
	for i, cmd := range executionList {
		if cmd.Name != expected[i] {
			t.Errorf("At index %d, expected %s, got %s", i, expected[i], cmd.Name)
		}
	}
}
