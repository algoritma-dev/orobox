package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestErrWritesOneLinePerMessageLine(t *testing.T) {
	var payload, errw bytes.Buffer
	restore := SetWriters(&payload, &errw)
	defer restore()

	Err("QA tools reported errors or warnings.")
	Err("invalid config file .orobox.yaml:\nfield type is required")

	want := strings.Join([]string{
		"error: QA tools reported errors or warnings.",
		"error: invalid config file .orobox.yaml:",
		"error: field type is required",
		"",
	}, "\n")

	if got := errw.String(); got != want {
		t.Errorf("Err() wrote %q, want %q", got, want)
	}
	if payload.Len() != 0 {
		t.Errorf("Err() wrote %q to the payload stream, want nothing", payload.String())
	}
}

func TestAgentDefaultsOff(t *testing.T) {
	if Agent() {
		t.Fatal("Agent() is true before SetAgent, want false")
	}
	SetAgent(true)
	defer SetAgent(false)
	if !Agent() {
		t.Error("Agent() is false after SetAgent(true)")
	}
}

func TestErrTrimsTrailingNewlines(t *testing.T) {
	var payload, errw bytes.Buffer
	restore := SetWriters(&payload, &errw)
	defer restore()

	Err("boom\n\n")

	if got, want := errw.String(), "error: boom\n"; got != want {
		t.Errorf("Err() wrote %q, want %q", got, want)
	}
}
