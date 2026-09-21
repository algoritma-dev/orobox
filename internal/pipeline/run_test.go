package pipeline

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/output"
)

// TestProgressWriterIsDiscardedInAgentMode is the regression for `orobox qa --agent` on the
// Dagger engine: the step banners and the streamed command output went to stdout whatever the
// mode was, so an automated caller read "▸ [deps] composer install" and "--- Running phpstan ---"
// where the contract promises one finding per line and nothing else.
func TestProgressWriterIsDiscardedInAgentMode(t *testing.T) {
	output.SetAgent(true)
	defer output.SetAgent(false)

	if got := progressWriter(); got != io.Discard {
		t.Errorf("progressWriter() = %T, want io.Discard", got)
	}
}

// TestProgressWriterIsStdoutForAHuman is the other half: the progress is the only thing a
// developer watching a twenty-minute pipeline has.
func TestProgressWriterIsStdoutForAHuman(t *testing.T) {
	output.SetAgent(false)

	if got := progressWriter(); got != os.Stdout {
		t.Errorf("progressWriter() = %T, want os.Stdout", got)
	}
}

// TestAgentModeWritesNothingToStdout checks the whole path rather than the writer alone: a
// reporter built the way Run builds it puts no banner, no streamed line and no timing on the
// process's stdout, which is the stream the caller parses.
func TestAgentModeWritesNothingToStdout(t *testing.T) {
	output.SetAgent(true)
	defer output.SetAgent(false)

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not open a pipe: %v", err)
	}
	real := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = real }()

	newReporter(progressWriter(), false, nil).
		start("qa", "php-cs-fixer fix").
		ok("--- Running php-cs-fixer ---")

	os.Stdout = real
	if err := write.Close(); err != nil {
		t.Fatalf("could not close the pipe: %v", err)
	}
	written, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("could not read the pipe: %v", err)
	}

	if strings.TrimSpace(string(written)) != "" {
		t.Errorf("agent mode wrote progress to stdout:\n%s", written)
	}
}
