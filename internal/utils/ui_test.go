package utils

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/algoritma-dev/orobox/internal/output"
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

func TestPrintHelpersAreSilentInAgentMode(t *testing.T) {
	var human bytes.Buffer
	restoreUtils := SetWriter(&human)
	defer restoreUtils()

	var payload, errs bytes.Buffer
	restoreOutput := output.SetWriters(&payload, &errs)
	defer restoreOutput()

	output.SetAgent(true)
	defer output.SetAgent(false)

	PrintSuccess("All selected QA tools passed!")
	PrintInfo("Running QA tools...")
	PrintWarning("No QA tools enabled.")
	PrintTitle("Summary")
	StartLoader("Starting the containers")
	StopLoader()
	PrintError("QA tools reported errors or warnings.")

	if human.Len() != 0 {
		t.Errorf("agent mode wrote %q to the human stream, want nothing", human.String())
	}
	if payload.Len() != 0 {
		t.Errorf("agent mode wrote %q to the payload stream, want nothing", payload.String())
	}
	if got, want := errs.String(), "error: QA tools reported errors or warnings.\n"; got != want {
		t.Errorf("agent mode wrote %q to stderr, want %q", got, want)
	}
}

func TestPrintHelpersKeepHumanOutputWhenAgentModeIsOff(t *testing.T) {
	var human bytes.Buffer
	restore := SetWriter(&human)
	defer restore()

	PrintSuccess("done")
	PrintError("boom")

	got := human.String()
	if !strings.Contains(got, "✔ done") {
		t.Errorf("PrintSuccess wrote %q, want it to contain %q", got, "✔ done")
	}
	if !strings.Contains(got, "✘ boom") {
		t.Errorf("PrintError wrote %q, want it to contain %q", got, "✘ boom")
	}
}

func TestAskFunctionsTakeDefaultsInAgentMode(t *testing.T) {
	var human bytes.Buffer
	restoreUtils := SetWriter(&human)
	defer restoreUtils()

	output.SetAgent(true)
	defer output.SetAgent(false)

	// A reader that would supply an answer, to prove it is never read.
	reader := bufio.NewReader(strings.NewReader("typed answer\ny\n2\n"))

	if got := AskQuestion(reader, "Bundle name", "acme"); got != "acme" {
		t.Errorf("AskQuestion() = %q, want the default %q", got, "acme")
	}
	if got, eof := AskQuestionOrEOF(reader, "Bundle name", "acme"); got != "acme" || !eof {
		t.Errorf("AskQuestionOrEOF() = (%q, %v), want (%q, true)", got, eof, "acme")
	}
	if got := AskYesNo(reader, "Overwrite?", false); got != false {
		t.Errorf("AskYesNo() = %v, want the default false", got)
	}
	if got := AskSelection(reader, "Type", []string{"project", "bundle"}, "bundle"); got != "bundle" {
		t.Errorf("AskSelection() = %q, want the default %q", got, "bundle")
	}
	if human.Len() != 0 {
		t.Errorf("agent mode printed the prompts: %q", human.String())
	}
}
