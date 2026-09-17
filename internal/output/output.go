// Package output decides how much of Orobox's own output reaches the caller.
//
// It exists as its own package rather than as a flag inside internal/utils because utils is what
// every other package prints through: a mode flag living there would make the same package both
// the gate and the thing being gated, and internal/docker, internal/pipeline and cmd would all be
// importing the gate to ask whether they are allowed to print.
//
// Nothing here imports anything of Orobox's own, which is what lets utils import it.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// agent is process-global because the mode is decided once, from a persistent flag, before any
// command runs. Threading it through every call site would touch ~290 of them to say the same
// thing at each.
var agent bool

var (
	payloadWriter io.Writer = os.Stdout
	errWriter     io.Writer = os.Stderr
)

// SetAgent records whether this process runs for an automated caller. Called once, from the root
// command's PersistentPreRun.
func SetAgent(on bool) { agent = on }

// Agent reports whether output must stay minimal.
func Agent() bool { return agent }

// Payload is the stream a command's actual result goes to — QA findings, test failures, the
// verbatim output of a passthrough command. Never diagnostics.
func Payload() io.Writer { return payloadWriter }

// SetWriters redirects both streams and returns a function restoring them, for tests.
func SetWriters(payload, errs io.Writer) func() {
	prevPayload, prevErr := payloadWriter, errWriter
	payloadWriter, errWriter = payload, errs
	return func() { payloadWriter, errWriter = prevPayload, prevErr }
}

// Err writes one diagnostic to stderr, one line per line of the message.
//
// Multi-line messages are real: a config validation failure arrives as the file name followed by
// each invalid field. Prefixing every line keeps a caller that splits on newlines from mistaking
// the continuation for payload.
func Err(msg string) {
	for _, line := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		fmt.Fprintf(errWriter, "error: %s\n", line)
	}
}
