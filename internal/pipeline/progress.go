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

	// now is the clock every duration in the progress output is measured against. Tests replace
	// it, because the backoff and the task timings are otherwise untestable.
	now func() time.Time
}

func newReporter(out io.Writer, verbose bool, log *logBuffer) *reporter {
	r := &reporter{out: out, verbose: verbose, now: time.Now}
	if log != nil {
		r.watch = newEngineWatch(log)
	}
	return r
}

// heartbeatDelays is how long a command may stay silent before it says so again. A build that
// prints nothing for half an hour should not print fifty identical lines, so the interval grows
// and then settles: 30s, 1m, 2m, then every 5m.
var heartbeatDelays = []time.Duration{
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
}

// heartbeatDelay returns the wait before the next beat, given how many silent beats have
// already happened in a row. Any output resets the caller's counter to zero.
func heartbeatDelay(quiet int) time.Duration {
	if quiet >= len(heartbeatDelays) {
		return heartbeatDelays[len(heartbeatDelays)-1]
	}
	return heartbeatDelays[quiet]
}

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
	// stopped is closed by the beat goroutine, so ok and fail can print after it has stopped
	// writing instead of racing it for the terminal.
	stopped chan struct{}
	// reader follows this command's live output in the engine log between heartbeats.
	reader *engineReader
	// tracker names the Deployer task a silence line is stuck on. Only the release step feeds
	// it, but every task has one so no call site needs a nil check.
	tracker *deployerTracker
	// stopping makes finish idempotent: ok and fail call it, and a test that wants to drive the
	// polls by hand calls it first to stop the goroutine.
	stopping sync.Once
	// stream prints every line as it arrives instead of a tail per heartbeat. Only the release
	// step does this: it runs alone, so nothing else can interleave with it.
	stream bool
}

// start announces a command and starts its heartbeat. Exactly one of ok/fail must be called.
func (r *reporter) start(step, command string) *task {
	r.write(fmt.Sprintf("%s▸ [%s]%s %s\n", progressCyan, step, progressReset, label(command)))

	t := &task{
		reporter: r,
		step:     step,
		command:  command,
		started:  r.now(),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
		tracker:  newDeployerTracker(),
	}
	if r.watch != nil && !r.verbose {
		// With --debug the engine log is streamed in full already, so quoting it again would
		// print every line twice.
		t.reader = r.watch.follow(command)
		t.stream = step == releaseStepName
	}
	if t.stream {
		go t.streamLoop()
	} else {
		go t.beat()
	}
	return t
}

// streamPoll is how often a streaming step drains its output. It is the latency between a line
// being written on the remote host and appearing here, so it is short; a drain that finds
// nothing costs one mutex and one buffer read.
const streamPoll = time.Second

func (t *task) streamLoop() {
	defer close(t.stopped)

	poll := time.NewTicker(streamPoll)
	defer poll.Stop()

	quiet := 0
	next := t.started.Add(heartbeatDelay(quiet))
	for {
		select {
		case <-t.done:
			return
		case <-poll.C:
			now := t.reporter.now()
			if t.pollStream(now) {
				quiet = 0
				next = now.Add(heartbeatDelay(quiet))
				continue
			}
			if now.Before(next) {
				continue
			}
			t.reporter.write(t.silence(now))
			quiet++
			next = now.Add(heartbeatDelay(quiet))
		}
	}
}

// pollStream prints everything the command has written since the last poll and reports whether
// there was any. Unlike pollTail it prints nothing when there is nothing: the silence line is
// the loop's business, because only the loop knows how long the quiet has lasted.
func (t *task) pollStream(now time.Time) bool {
	lines := t.reader.drain(now)
	if len(lines) == 0 {
		return false
	}

	var block strings.Builder
	for _, line := range lines {
		t.tracker.observe(line, now)
		block.WriteString("    " + line + "\n")
	}
	t.reporter.write(block.String())
	return true
}

func (t *task) beat() {
	defer close(t.stopped)

	quiet := 0
	timer := time.NewTimer(heartbeatDelay(quiet))
	defer timer.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-timer.C:
			if t.pollTail(t.reporter.now()) {
				quiet = 0
			} else {
				quiet++
			}
			timer.Reset(heartbeatDelay(quiet))
		}
	}
}

// pollTail prints one beat of a non-streaming step: the tail of whatever arrived since the last
// one, or the silence line when nothing did. It reports whether there was output, which is what
// resets the backoff.
func (t *task) pollTail(now time.Time) bool {
	lines := t.reader.next(heartbeatTailLines, now)
	if len(lines) == 0 {
		t.reporter.write(t.silence(now))
		return false
	}

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s  … [%s] still running: %s (%s)%s\n",
		progressDim, t.step, label(t.command), elapsed(t.started, now), progressReset))
	for _, line := range lines {
		block.WriteString(fmt.Sprintf("%s      %s%s\n", progressDim, line, progressReset))
	}
	t.reporter.write(block.String())
	return true
}

// silence is what a command that is producing nothing says. Repeating the command name would
// add nothing, so the line carries what the reader cannot see: how long the quiet has lasted
// and the last thing that was said.
func (t *task) silence(now time.Time) string {
	where := fmt.Sprintf("[%s]", t.step)
	if name := t.tracker.currentTask(); name != "" {
		where += " " + name + " —"
	}

	line, at, ok := t.reader.lastSeen()
	if !ok {
		return fmt.Sprintf("%s  … %s no output yet (running for %s)%s\n",
			progressDim, where, elapsed(t.started, now), progressReset)
	}
	return fmt.Sprintf("%s  … %s no output for %s (last: %s)%s\n",
		progressDim, where, elapsed(at, now), truncate(line, commandLabelLimit), progressReset)
}

// truncate keeps a quoted line inside one terminal line.
func truncate(text string, limit int) string {
	if len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

// finish stops the beat goroutine and waits for it, so the closing block is the last thing this
// task writes. Calling it twice is allowed: ok and fail call it, and so does a test that wants
// the goroutine out of the way before driving the polls by hand.
func (t *task) finish() {
	t.stopping.Do(func() {
		close(t.done)
		<-t.stopped
	})
}

// ok prints the command's own output, cleaned of progress-bar redraws and trimmed to the tail
// unless --debug is on. A streaming step has printed it all already, so it closes with the
// timings instead.
func (t *task) ok(output string) {
	t.finish()
	now := t.reporter.now()

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s✔ [%s]%s %s %s(%s)%s\n",
		progressGreen, t.step, progressReset, label(t.command), progressDim, elapsed(t.started, now), progressReset))
	if t.stream {
		// Whatever the last poll missed is still worth printing, and only a streaming step can
		// print it: for the others the closing body below is the command's whole output, so
		// draining the reader too would show the tail twice.
		t.pollStream(now)
		if summary := t.tracker.summary(now); summary != "" {
			block.WriteString(fmt.Sprintf("%s    %s%s\n", progressDim, summary, progressReset))
		}
	} else if body := t.reporter.body(output); body != "" {
		block.WriteString(body + "\n")
	}
	t.reporter.write(block.String())
}

// fail marks a command as failed. The output itself is not printed here: the error report
// built by describe carries it, together with the exit code. The timings still are, because
// they say which Deployer task the failure happened in and how long it had been running.
func (t *task) fail() {
	t.finish()
	now := t.reporter.now()

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s✘ [%s]%s %s %s(%s)%s\n",
		progressRed, t.step, progressReset, label(t.command), progressDim, elapsed(t.started, now), progressReset))
	if t.stream {
		t.pollStream(now)
		if summary := t.tracker.summary(now); summary != "" {
			block.WriteString(fmt.Sprintf("%s    %s%s\n", progressDim, summary, progressReset))
		}
	}
	t.reporter.write(block.String())
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

// elapsed renders how long ago from was, measured against the reporter's clock.
func elapsed(from, to time.Time) string {
	return humanDuration(to.Sub(from))
}
