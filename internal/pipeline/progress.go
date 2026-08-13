package pipeline

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// commandLabelLimit caps the one-line summary printed for a command. Full command lines are
// long enough (composer requires, phpstan invocations) to wrap several terminal lines, which
// defeats the purpose of a progress line.
const commandLabelLimit = 100

// outputTailLines is how many lines of a command's own output are printed without --debug.
// Enough for a PHPUnit summary or the end of a composer run, short enough to stay readable.
const outputTailLines = 30

const (
	progressReset = "\033[0m"
	progressDim   = "\033[90m"
	progressCyan  = "\033[36m"
	progressGreen = "\033[32m"
	progressRed   = "\033[31m"
)

// reporter prints what the pipeline is doing, one block per command: a line when the command
// starts and its output when it ends. The QA and test steps run concurrently, so every block
// is written under the mutex and carries its step name.
type reporter struct {
	mu sync.Mutex
	// out is where the progress goes; the engine log keeps using its own writer.
	out io.Writer
	// verbose prints every line of output instead of the tail.
	verbose bool

	// watch turns the engine log into per-command output a heartbeat can quote. Nil when there
	// is no engine log to read, which is what the tests of the block format use.
	watch *engineWatch
}

func newReporter(out io.Writer, verbose bool, log *logBuffer) *reporter {
	r := &reporter{out: out, verbose: verbose}
	if log != nil {
		r.watch = newEngineWatch(log)
	}
	return r
}

// heartbeat is how often a command that is still running says so. A container build can take
// minutes with nothing to show, which is exactly when a silent terminal looks like a hang.
const heartbeat = 30 * time.Second

// heartbeatTailLines caps how much of a running command's output one heartbeat prints. A chatty
// command would otherwise turn every progress line into a log dump, which is what --debug is for.
const heartbeatTailLines = 8

// task is one running command. It keeps the terminal alive while the command runs and prints
// the closing block when it ends.
type task struct {
	reporter *reporter
	step     string
	command  string
	started  time.Time
	done     chan struct{}
	// reader follows this command's live output in the engine log between heartbeats.
	reader *engineReader
}

// start announces a command and starts its heartbeat. Exactly one of ok/fail must be called.
func (r *reporter) start(step, command string) *task {
	r.write(fmt.Sprintf("%s▸ [%s]%s %s\n", progressCyan, step, progressReset, label(command)))

	t := &task{reporter: r, step: step, command: command, started: time.Now(), done: make(chan struct{})}
	if r.watch != nil && !r.verbose {
		// With --debug the engine log is streamed in full already, so quoting it again would
		// print every line twice.
		t.reader = r.watch.follow(command)
	}
	go t.beat()
	return t
}

func (t *task) beat() {
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			var block strings.Builder
			block.WriteString(fmt.Sprintf("%s  … [%s] still running: %s (%s)%s\n",
				progressDim, t.step, label(t.command), elapsed(t.started), progressReset))
			for _, line := range t.reader.next(heartbeatTailLines) {
				block.WriteString(fmt.Sprintf("%s      %s%s\n", progressDim, line, progressReset))
			}
			t.reporter.write(block.String())
		}
	}
}

// ok prints the command's own output, cleaned of progress-bar redraws and trimmed to the tail
// unless --debug is on.
func (t *task) ok(output string) {
	close(t.done)

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s✔ [%s]%s %s %s(%s)%s\n",
		progressGreen, t.step, progressReset, label(t.command), progressDim, elapsed(t.started), progressReset))
	if body := t.reporter.body(output); body != "" {
		block.WriteString(body + "\n")
	}
	t.reporter.write(block.String())
}

// fail marks a command as failed. The output itself is not printed here: the error report
// built by describe carries it, together with the exit code.
func (t *task) fail() {
	close(t.done)
	t.reporter.write(fmt.Sprintf("%s✘ [%s]%s %s %s(%s)%s\n",
		progressRed, t.step, progressReset, label(t.command), progressDim, elapsed(t.started), progressReset))
}

func (r *reporter) write(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprint(r.out, text)
}

// body renders a command's output as an indented block, dropping what a terminal-oriented
// tool wrote for a human watching it live.
func (r *reporter) body(output string) string {
	lines := cleanOutput(output)
	if len(lines) == 0 {
		return ""
	}

	var hidden int
	if !r.verbose && len(lines) > outputTailLines {
		hidden = len(lines) - outputTailLines
		lines = lines[hidden:]
	}

	var b strings.Builder
	if hidden > 0 {
		b.WriteString(fmt.Sprintf("%s    ... %d earlier lines hidden (--debug prints them all)%s\n",
			progressDim, hidden, progressReset))
	}
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("    " + line)
	}
	return b.String()
}

// cleanOutput turns raw command output into printable lines: carriage-return redraws are
// collapsed to their final state, blank runs to a single blank line, and leading/trailing
// blanks are dropped.
func cleanOutput(output string) []string {
	var lines []string
	blank := false
	for _, raw := range strings.Split(output, "\n") {
		// A progress bar rewrites the same line with \r; only the last state is meaningful.
		if idx := strings.LastIndex(raw, "\r"); idx >= 0 {
			raw = raw[idx+1:]
		}
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			// Collapse runs of blank lines, and never start the block with one.
			if blank || len(lines) == 0 {
				continue
			}
			blank = true
			lines = append(lines, "")
			continue
		}
		blank = false
		lines = append(lines, line)
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// label shortens a command to a single readable line. Some steps run small shell scripts
// (the generated PHPStan and Twig configs), so the first line that actually does something is
// what identifies the command, not the whole script.
func label(command string) string {
	first := command
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "set -e" {
			continue
		}
		first = trimmed
		break
	}

	first = strings.Join(strings.Fields(first), " ")
	if len([]rune(first)) > commandLabelLimit {
		first = string([]rune(first)[:commandLabelLimit-1]) + "…"
	}
	return first
}

func elapsed(started time.Time) string {
	d := time.Since(started).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
