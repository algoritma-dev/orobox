// Package utils provides utility functions for user interaction.
package utils

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/algoritma-dev/orobox/internal/output"
)

var (
	loaderStop chan struct{}
	loaderWG   sync.WaitGroup
	loaderMu   sync.Mutex
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

// out is where the human-facing helpers write. It is a variable so tests can capture what a
// command printed, which is the only way to assert that agent mode printed nothing.
var out io.Writer = os.Stdout

// SetWriter redirects the print helpers and returns a function restoring the previous writer.
func SetWriter(w io.Writer) func() {
	prev := out
	out = w
	return func() { out = prev }
}

// PrintPlain writes one line verbatim: no glyph, no colour, nothing prepended.
//
// It exists for the blocks of URLs, credentials and .env snippets the commands print as a unit,
// where a per-line glyph would be noise. Gating those through here rather than through fmt.Println
// is what lets agent mode drop them; the output a human sees is unchanged.
func PrintPlain(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintln(out, message)
}

// PrintPlainf is PrintPlain with a format string. The format supplies its own newline, exactly as
// the fmt.Printf calls it replaces did.
func PrintPlainf(format string, a ...any) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, format, a...)
}

// PrintSuccess prints a success message in green. Agent mode drops it: a caller that reads the
// exit code does not need to be told the exit code was zero.
func PrintSuccess(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, "%s✔ %s%s\n", colorGreen, message, colorReset)
}

// PrintError prints an error message in red. Agent mode is the one case that still prints: an
// error is the only thing an automated caller cannot reconstruct from the exit code.
func PrintError(message string) {
	if output.Agent() {
		output.Err(message)
		return
	}
	fmt.Fprintf(out, "%s✘ %s%s\n", colorRed, message, colorReset)
}

// PrintWarning prints a warning message in yellow. Dropped in agent mode: a warning is by
// definition something the run survived, so the caller can act on the result without it.
func PrintWarning(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, "%s⚠ %s%s\n", colorYellow, message, colorReset)
}

// PrintInfo prints an informational message in blue. Dropped in agent mode.
func PrintInfo(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, "%sℹ %s%s\n", colorBlue, message, colorReset)
}

// PrintTitle prints a title message in cyan. Dropped in agent mode: a section header structures
// output for a human reading it scroll past, which is not what agent mode produces.
func PrintTitle(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, "\n%s%s%s\n", colorCyan, message, colorReset)
}

// AskQuestion asks a question to the user and returns the answer or a default value.
func AskQuestion(reader *bufio.Reader, question string, defaultValue string) string {
	answer, _ := AskQuestionOrEOF(reader, question, defaultValue)
	return answer
}

// AskQuestionOrEOF is AskQuestion plus whether the reader is exhausted.
//
// The second return value is what lets a caller re-ask for a required value without hanging: a
// non-interactive run — a script, a CI job, the e2e harness — hits EOF on the first read, so a
// loop that only checked for an empty answer would never end.
func AskQuestionOrEOF(reader *bufio.Reader, question string, defaultValue string) (string, bool) {
	// Agent mode never prompts. The eof result is what a caller looping for a required value
	// tests, so returning true here ends that loop with the same "nothing more is coming" answer
	// a closed stdin gives — see SkipPrompts.
	if output.Agent() {
		return defaultValue, true
	}
	fmt.Fprintf(out, "%s%s%s [%s]: ", colorCyan, question, colorReset, defaultValue)
	input, err := reader.ReadString('\n')
	eof := errors.Is(err, io.EOF)
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue, eof
	}
	return input, eof
}

// AskYesNo asks a yes/no question to the user and returns the boolean response.
func AskYesNo(reader *bufio.Reader, question string, defaultValue bool) bool {
	// Agent mode takes the default rather than prompting. A yes/no question an automated caller
	// cannot see would block the run until its stdin closed.
	if output.Agent() {
		return defaultValue
	}
	defaultStr := "y"
	if !defaultValue {
		defaultStr = "n"
	}
	fmt.Fprintf(out, "%s%s (y/n)%s [%s]: ", colorCyan, question, colorReset, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultValue
	}
	return input == "y" || input == "yes"
}

// SkipPrompts reports whether questions reading from r must be answered with their defaults
// instead of read.
//
// Every Ask* function blocks in ReadString until a newline arrives, so it falls back to its
// default only once the reader is at EOF. That is fine for a terminal, where a human supplies
// the newline, and fine for an in-memory reader, which is already complete and reaches EOF on
// its own. It is not fine for a non-terminal *os.File: a CI or daemon process may inherit a
// pipe nothing ever writes to and never closes, and the prompt would hang there forever.
// Those are the only readers this reports true for; the caller then takes the default without
// reading, which is what a closed stdin already produced.
func SkipPrompts(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return !term.IsTerminal(int(f.Fd()))
}

// AskSelection asks a multiple choice question to the user and returns the selected value.
func AskSelection(reader *bufio.Reader, question string, options []string, defaultValue string) string {
	// Agent mode takes the default. Printing the options would cost the caller a line each to
	// describe a choice it is not being offered.
	if output.Agent() {
		return defaultValue
	}
	fmt.Fprintf(out, "%s%s%s\n", colorCyan, question, colorReset)
	for i, option := range options {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, option)
	}

	defaultIdx := -1
	for i, option := range options {
		if option == defaultValue {
			defaultIdx = i + 1
			break
		}
	}

	if defaultIdx != -1 {
		fmt.Fprintf(out, "Selection [%d]: ", defaultIdx)
	} else {
		fmt.Fprint(out, "Selection: ")
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" && defaultIdx != -1 {
		return defaultValue
	}

	var idx int
	_, err := fmt.Sscanf(input, "%d", &idx)
	if err != nil || idx < 1 || idx > len(options) {
		fmt.Fprintln(out, "Invalid selection, please try again.")
		return AskSelection(reader, question, options, defaultValue)
	}

	return options[idx-1]
}

// stdoutIsTerminal reports whether stdout is a terminal. Overridable in tests.
var stdoutIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StartLoader starts a spinner loader with a message.
//
// The spinner repaints one line with a carriage return, which only works on a terminal.
// Piped or captured output (CI logs, the e2e harness) keeps every frame instead: ten lines a
// second for as long as the step runs, which buries the real output and, in a long install,
// alone exceeds a GitHub Actions step log. So off a terminal the message is printed once and
// no spinner runs.
func StartLoader(message string) {
	loaderMu.Lock()
	defer loaderMu.Unlock()

	if loaderStop != nil {
		return // Loader already running
	}

	// Agent mode starts no spinner and prints no fallback message. The spinner is a human's
	// reassurance that a long step is still alive; an automated caller reads the result.
	if output.Agent() {
		return
	}

	if !stdoutIsTerminal() {
		if message != "" {
			fmt.Fprintln(out, message)
		}
		return
	}

	loaderStop = make(chan struct{})
	stopCh := loaderStop
	loaderWG.Add(1)

	go func() {
		defer loaderWG.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				fmt.Fprint(out, "\r\033[K") // Clear the line
				return
			case <-ticker.C:
				fmt.Fprintf(out, "\r%s%s %s%s", colorCyan, frames[i], message, colorReset)
				i = (i + 1) % len(frames)
			}
		}
	}()
}

// StopLoader stops the current spinner loader.
func StopLoader() {
	loaderMu.Lock()
	if loaderStop != nil {
		close(loaderStop)
		loaderStop = nil
	}
	loaderMu.Unlock()
	loaderWG.Wait()
}
