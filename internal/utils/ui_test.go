package utils

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoader(t *testing.T) {
	StartLoader("Testing...")

	done := make(chan bool)
	go func() {
		StopLoader()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("StopLoader timed out, possible deadlock")
	}
}

func TestAskQuestion(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue string
		want         string
	}{
		{
			name:         "with input",
			input:        "custom value\n",
			defaultValue: "default",
			want:         "custom value",
		},
		{
			name:         "empty input",
			input:        "\n",
			defaultValue: "default",
			want:         "default",
		},
		{
			name:         "with spaces",
			input:        "  custom value  \n",
			defaultValue: "default",
			want:         "custom value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskQuestion(reader, "test question", tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskQuestion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskYesNo(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue bool
		want         bool
	}{
		{
			name:         "yes input",
			input:        "y\n",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "yes full input",
			input:        "yes\n",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "no input",
			input:        "n\n",
			defaultValue: true,
			want:         false,
		},
		{
			name:         "empty input uses default true",
			input:        "\n",
			defaultValue: true,
			want:         true,
		},
		{
			name:         "empty input uses default false",
			input:        "\n",
			defaultValue: false,
			want:         false,
		},
		{
			name:         "mixed case input",
			input:        "YeS\n",
			defaultValue: false,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskYesNo(reader, "test question", tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskYesNo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskSelection(t *testing.T) {
	options := []string{"7.0", "6.1", "6.0"}
	tests := []struct {
		name         string
		input        string
		defaultValue string
		want         string
	}{
		{
			name:         "valid selection 1",
			input:        "1\n",
			defaultValue: "6.1",
			want:         "7.0",
		},
		{
			name:         "valid selection 2",
			input:        "2\n",
			defaultValue: "7.0",
			want:         "6.1",
		},
		{
			name:         "empty input uses default",
			input:        "\n",
			defaultValue: "6.0",
			want:         "6.0",
		},
		{
			name:         "invalid then valid selection",
			input:        "99\n3\n",
			defaultValue: "7.0",
			want:         "6.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskSelection(reader, "Select version", options, tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskSelection() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A non-terminal *os.File is the one reader a prompt can hang on forever: an inherited pipe
// may never be written to and never reach EOF. In-memory readers are complete already and a
// terminal has a human on the other end, so neither is skipped.
func TestSkipPrompts(t *testing.T) {
	// The test seam every cmd package test installs over stdin. Skipping it would silently
	// stop those tests from answering anything.
	if SkipPrompts(strings.NewReader("y\n")) {
		t.Error("an in-memory reader reaches EOF on its own and must still be read")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	defer r.Close()
	defer w.Close()
	// Nothing is written to w and nothing closes it: reading a line here would block for as
	// long as the process lives.
	if !SkipPrompts(r) {
		t.Error("an open pipe must be skipped rather than read")
	}

	// What a CI runner and the e2e harness actually hand the process.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	if !SkipPrompts(devNull) {
		t.Errorf("%s must be skipped rather than read", os.DevNull)
	}
}
