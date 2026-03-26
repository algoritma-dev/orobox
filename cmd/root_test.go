package cmd

import (
	"bytes"
	"testing"
)

func TestRootCommand(t *testing.T) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if rootCmd.Use != "oro" {
		t.Errorf("Expected use 'oro', got %s", rootCmd.Use)
	}

	if rootCmd.Version != Version {
		t.Errorf("Expected version %s, got %s", Version, rootCmd.Version)
	}
}

func TestVersionFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	got := buf.String()
	want := "oro version " + Version + "\n"
	if got != want {
		t.Errorf("Expected %q, got %q", want, got)
	}
}
