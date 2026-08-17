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

// outputTailLines caps the fallback block: the captured output of a command whose live lines
// never reached the terminal. Enough for a PHPUnit summary or the end of a composer run, short
// enough to stay readable.
const outputTailLines = 30

const (
	progressReset = "\033[0m"
	progressDim   = "\033[90m"
	progressCyan  = "\033[36m"
	progressGreen = "\033[32m"
	progressRed   = "\033[31m"
)

// reporter prints what the pipeline is doing: a line when a command starts, its own output as it
// arrives, and a line with the timings when it ends. The QA and test steps run concurrently, so
// every block is written under the mutex and says which step it belongs to.
type reporter struct {
	mu sync.Mutex
	// out is where the progress goes; the engine log keeps using its own writer.
	out io.Writer
	// verbose prints every line of output instead of the tail.
	verbose bool
	// lastStep is the step whose block was written last. Streamed output carries no step name of
	// its own, so a marker is printed whenever it changes and consecutive lines are otherwise
	// known to belong to the same command.
	lastStep string

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
	// stream prints every line as it arrives. It is off only when there is no engine log to read
	// or when --debug is streaming it in full already.
	stream bool
	// printed counts the lines streamed so far. A command whose span the watcher never matched
	// streams nothing, and the closing block then falls back to printing its captured output.
	printed int
}

// start announces a command and begins following its output. Exactly one of ok/fail must be
// called.
func (r *reporter) start(step, command string) *task {
	r.write(step, fmt.Sprintf("%s▸ [%s]%s %s\n", progressCyan, step, progressReset, label(command)))

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
		t.stream = true
	}
	go t.loop()
	return t
}

// streamPoll is how often a running command's output is drained. It is the latency between a
// line being written inside the container and appearing here, so it is short; a drain that finds
// nothing costs one mutex and one buffer read.
const streamPoll = time.Second

// loop drains the running command's output every second and, when there is none, says so on the
// backoff schedule. A task with no reader behind it — no engine log, or --debug streaming it
// already — drains nothing and only ever prints the silence lines.
func (t *task) loop() {
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
			t.reporter.write(t.step, t.silence(now))
			quiet++
			next = now.Add(heartbeatDelay(quiet))
		}
	}
}

// pollStream prints everything the command has written since the last poll and reports whether
// there was any. It prints nothing when there is nothing: the silence line is the loop's
// business, because only the loop knows how long the quiet has lasted.
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
	t.printed += len(lines)
	t.reporter.writeOutput(t.step, block.String())
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

// ok closes a command that succeeded. Everything it wrote has been streamed line by line
// already, so the closing block carries only the timings. The captured output is the fallback
// for a command the watcher never found a span for, which would otherwise end without ever
// having shown anything.
//
// A command the engine served from its cache takes neither path. Its output is genuine — it is what
// the command printed the last time it really ran, kept in the cache with the rest of the exec's
// result and replayed verbatim by the SDK — and printing it says "composer installed the
// dependencies" about a run in which composer was never started. So the cache is named instead and
// the output left out.
func (t *task) ok(output string) {
	t.finish()
	now := t.reporter.now()

	// Whatever the last poll missed is printed before the closing line, so the block still reads
	// in the order the command wrote it.
	if t.stream {
		t.pollStream(now)
	}

	// Before the closing line, because it decides what that line says.
	cached := t.cachedByEngine()

	suffix := elapsed(t.started, now)
	if cached {
		suffix += ", from cache"
	}

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s✔ [%s]%s %s %s(%s)%s\n",
		progressGreen, t.step, progressReset, label(t.command), progressDim, suffix, progressReset))
	switch {
	case cached:
		// Nothing to add: the command did not run, so it has no output of this run to show.
	case t.streamed():
		if summary := t.tracker.summary(now); summary != "" {
			block.WriteString(fmt.Sprintf("%s    %s%s\n", progressDim, summary, progressReset))
		}
	default:
		if body := t.reporter.body(output); body != "" {
			block.WriteString(body + "\n")
		}
	}
	t.reporter.write(t.step, block.String())
}

// cacheStatusWait bounds how long the closing block waits for the engine to say what became of a
// command. The engine writes its progress concurrently with the SDK call that returns the output,
// and for a cached exec it writes all of it afterwards: measured against a real engine, the span
// appears and closes some 180ms after Stdout returns. The budget is an order of magnitude above
// that, because being slow to admit a cache hit is better than claiming a command ran, and still
// bounded, because a run must not hang on one unlucky span.
const (
	cacheStatusWait = 2 * time.Second
	cacheStatusPoll = 25 * time.Millisecond
)

// cachedByEngine reports whether the engine served this command from its cache instead of running
// it. A command whose live output was streamed plainly ran, so it is answered without asking.
//
// The wait covers a span that is not merely status-less but altogether absent: that is the normal
// state of a cached exec at this point, and returning early on it would answer "not cached" for
// every one of them. A reader with no key is the one case nothing will ever arrive for — the
// command is too short to be matched against the engine's rendering — so it is not waited for.
//
// The wait is measured against the real clock and not the reporter's: it is a race with a
// concurrent writer rather than a duration anything reports.
func (t *task) cachedByEngine() bool {
	if t.streamed() || t.reader == nil || t.reader.key == "" {
		return false
	}

	deadline := time.Now().Add(cacheStatusWait)
	for {
		if status, _ := t.reader.status(); status != "" {
			return status == engineStatusCached
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(cacheStatusPoll)
	}
}

// streamed reports whether this command's output reached the terminal as it ran. It did not when
// there is no engine log behind the task, and it did not when the command's span was never
// matched — a short or heavily quoted command line has no key distinctive enough to bind one.
func (t *task) streamed() bool {
	return t.stream && t.printed > 0
}

// fail marks a command as failed. The output itself is not printed here: the error report
// built by describe carries it, together with the exit code. The timings still are, because
// they say which Deployer task the failure happened in and how long it had been running.
func (t *task) fail() {
	t.finish()
	now := t.reporter.now()

	if t.stream {
		t.pollStream(now)
	}

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s✘ [%s]%s %s %s(%s)%s\n",
		progressRed, t.step, progressReset, label(t.command), progressDim, elapsed(t.started, now), progressReset))
	if t.streamed() {
		if summary := t.tracker.summary(now); summary != "" {
			block.WriteString(fmt.Sprintf("%s    %s%s\n", progressDim, summary, progressReset))
		}
	}
	t.reporter.write(t.step, block.String())
}

// write prints a block that names its own step, so nothing has to be prefixed.
func (r *reporter) write(step, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastStep = step
	fmt.Fprint(r.out, text)
}

// writeOutput prints a block of a command's own output. The lines carry nothing that says whose
// they are, and the QA and test steps run at the same time, so a marker is printed whenever the
// step changes: between two markers every line belongs to the same command.
func (r *reporter) writeOutput(step, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastStep != step {
		fmt.Fprintf(r.out, "%s  ┄ [%s]%s\n", progressDim, step, progressReset)
		r.lastStep = step
	}
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

// label shortens a command to a single readable line. Some steps run small shell scripts (the
// test database restore, the QA warmup, the generated PHPStan and Twig configs), and their first
// executable line describes none of what follows: a script whose eleven minutes are an Oro
// install opens with an apk call. Those scripts title themselves with a leading comment, which
// is what the progress lines then name them by; anything else is identified by the first line
// that actually does something.
func label(command string) string {
	if title := commandTitle(command); title != "" {
		return truncate(title, commandLabelLimit)
	}
	return truncate(strings.Join(strings.Fields(firstCommandLine(command)), " "), commandLabelLimit)
}

// commandTitle returns the name a script gives itself in a comment on its first line, empty
// when it does not. Only the first line counts: a comment further down documents that line,
// not the script.
func commandTitle(command string) string {
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "#"); ok {
			return strings.TrimSpace(rest)
		}
		return ""
	}
	return ""
}

// firstCommandLine is the first line of a script that runs something. The title comment and the
// shell bookkeeping around it identify nothing, and the engine's rendering of the arguments is
// matched against this too, so it must be a line the command really contains.
func firstCommandLine(command string) string {
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "set -e" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return command
}

// elapsed renders how long ago from was, measured against the reporter's clock.
func elapsed(from, to time.Time) string {
	return humanDuration(to.Sub(from))
}
