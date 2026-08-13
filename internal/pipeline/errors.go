package pipeline

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"dagger.io/dagger"
)

// logTailLines is how much of the engine output a failure reports. Enough to show the failing
// command's output, short enough to stay readable in a terminal.
const logTailLines = 40

// logTailBytes caps what the buffer keeps in memory; the engine can be very verbose.
const logTailBytes = 256 * 1024

// outputLimit caps a single stream (stdout or stderr) in an error report.
const outputLimit = 4000

// logBuffer keeps the tail of the Dagger engine output so a failed run can explain itself
// without flooding a successful one. With --debug it also streams straight through.
type logBuffer struct {
	mu   sync.Mutex
	data []byte
	tee  io.Writer
	// partial holds an incomplete trailing line, so the tee can filter whole lines only.
	partial []byte
	// written counts every byte ever received, so a reader can ask for what appeared after a
	// given point even though data only keeps the tail.
	written int64
}

func newLogBuffer(tee io.Writer) *logBuffer {
	return &logBuffer{tee: tee}
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.written += int64(len(p))
	b.data = append(b.data, p...)
	if len(b.data) > logTailBytes {
		b.data = b.data[len(b.data)-logTailBytes:]
	}

	var forward []byte
	if b.tee != nil {
		// Image pull and registry traffic is noise even with --debug: it drowns out the pipeline
		// output someone turned --debug on to read.
		b.partial = append(b.partial, p...)
		for {
			end := bytesIndexNewline(b.partial)
			if end < 0 {
				break
			}
			line := string(b.partial[:end])
			b.partial = b.partial[end+1:]
			if !isEngineBoilerplate(line) {
				forward = append(forward, line...)
				forward = append(forward, '\n')
			}
		}
	}
	b.mu.Unlock()

	if len(forward) > 0 {
		if _, err := b.tee.Write(forward); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func bytesIndexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// Since returns the complete lines that arrived after offset, together with the offset to pass
// to the next call. It is what lets a heartbeat show a running command's progress: the SDK hands
// a command's own output over only when it exits, while the engine log arrives live.
//
// Output older than the retained tail is gone; the caller then resumes from the oldest byte
// still buffered rather than reporting nothing.
func (b *logBuffer) Since(offset int64) ([]string, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	oldest := b.written - int64(len(b.data))
	if offset < oldest {
		offset = oldest
	}
	window := b.data[offset-oldest:]

	// Stop at the last newline: a line still being written is printed by the next call, whole.
	end := -1
	for i := len(window) - 1; i >= 0; i-- {
		if window[i] == '\n' {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, offset
	}
	return strings.Split(string(window[:end]), "\n"), offset + int64(end) + 1
}

// Offset returns the current end of the log, so a reader can start from now instead of
// replaying what was written before it began watching.
func (b *logBuffer) Offset() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

// Tail returns the last logTailLines lines of engine output.
func (b *logBuffer) Tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	lines := strings.Split(strings.TrimRight(string(b.data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}

	var kept []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || isEngineBoilerplate(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// engineBoilerplate is engine bookkeeping: session startup and the containerd/registry traffic
// emitted while pulling images. Reporting it as "what went wrong" is worse than reporting
// nothing, and it is what fills the tail when a pull is in flight.
var engineBoilerplate = []string{
	"Creating new Engine session",
	"Establishing connection to Engine",
	"Connected to engine",
	"Cleaning up",
	"remotes.docker.resolver",
	"HTTP GET",
	"HTTP HEAD",
}

func isEngineBoilerplate(line string) bool {
	lower := strings.ToLower(line)
	for _, needle := range engineBoilerplate {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}

	// A line that is only a span id plus a DONE marker and a duration carries nothing.
	if spanNoise.MatchString(line) {
		return true
	}

	// Image pull progress. Both a pull verb and a registry reference or digest are required, so
	// real output such as "composer: could not resolve dependencies" is never mistaken for it.
	return pullNoise.MatchString(line)
}

var (
	spanNoise = regexp.MustCompile(`^\s*[┆|\s]*\d+\s*:\s*(DONE)?\s*(\[[\d.]+m?s\])?\s*$`)
	pullNoise = regexp.MustCompile(`(?i)\b(resolve|resolving|fetch|fetching|extract|extracting|unpack|unpacking|pull|pulling)\b.*(sha256:|docker\.io|ghcr\.io|quay\.io|registry\.)`)
)

// describe turns a Dagger failure into something actionable: which stage failed, which command,
// its exit code and output, and the tail of the engine log. Dagger's own message is a single
// line like "exit status 128" plus a traceparent, which says nothing on its own.
func (r *runner) describe(stage string, err error) error {
	if err == nil {
		return nil
	}

	var report strings.Builder
	report.WriteString(stage + " failed")

	hasOutput := false
	var execErr *dagger.ExecError
	if errors.As(err, &execErr) {
		if len(execErr.Cmd) > 0 {
			report.WriteString("\n  command:   " + strings.Join(execErr.Cmd, " "))
		}
		report.WriteString(fmt.Sprintf("\n  exit code: %d", execErr.ExitCode))
		if out := trimOutput(execErr.Stderr); out != "" {
			report.WriteString("\n  stderr:\n" + indent(out))
			hasOutput = true
		}
		if out := trimOutput(execErr.Stdout); out != "" {
			report.WriteString("\n  stdout:\n" + indent(out))
			hasOutput = true
		}
	} else {
		report.WriteString(": " + stripTraceparent(err.Error()))
	}

	// The engine log is a fallback: when the failing command's own output is available it says
	// everything, and appending engine internals only buries it.
	if !hasOutput {
		if tail := r.log.Tail(); tail != "" {
			report.WriteString("\n  engine log (last lines):\n" + indent(tail))
		}
	}

	if !r.opts.Debug {
		report.WriteString("\n  Re-run with --debug to print every command's full output.")
	}

	return errors.New(report.String())
}

// cloneError explains a failed checkout, which is nearly always credentials or a ref that does
// not exist. Dagger reports it as a bare "git error: exit status 128".
func (r *runner) cloneError(err error) error {
	hints := []string{
		fmt.Sprintf("check that the ref %q exists in %s", r.plan.Ref, r.plan.Repository),
	}
	if r.plan.SourceDir != "" {
		hints = append(hints, fmt.Sprintf("the build is rooted at deploy.source_dir %q: check that directory exists in the repository at this ref", r.plan.SourceDir))
	}
	if r.sshSocket != nil {
		hints = append(hints, "the repository is cloned through your SSH agent: make sure the key with access to it is loaded (ssh-add -l)")
		if r.knownHosts() == "" {
			hints = append(hints, "no known_hosts was available, so the git server's host key could not be verified inside the pipeline container: connect to it once with ssh so the key lands in ~/.ssh/known_hosts")
		}
	} else if r.opts.GitHTTPToken != "" {
		hints = append(hints, "the repository is cloned over https with a token: check OROBOX_DEPLOY_GIT_TOKEN / CI_JOB_TOKEN and its scope")
	} else {
		hints = append(hints, "no clone credentials were available: start an SSH agent, or set OROBOX_DEPLOY_GIT_TOKEN for an https URL")
	}
	if strings.HasPrefix(r.plan.Repository, "http") && r.sshSocket != nil && r.opts.GitHTTPToken == "" {
		hints = append(hints, "the URL is https but only an SSH agent is available; either use the SSH URL or set OROBOX_DEPLOY_GIT_TOKEN")
	}

	described := r.describe(fmt.Sprintf("cloning %s at %s", r.plan.Repository, r.plan.Ref), err)

	var report strings.Builder
	report.WriteString(described.Error())
	for _, hint := range hints {
		report.WriteString("\n  hint: " + hint)
	}
	return errors.New(report.String())
}

// trimOutput keeps the end of a stream, which is where the actual error usually is, plus any
// line from the dropped part that names a failure. A command such as oro:install prints a long
// requirements table and only summarises it at the end ("Found 1 not fulfilled requirement"):
// the row that failed sits in the middle and a plain tail throws away the one line that matters.
func trimOutput(out string) string {
	out = strings.TrimSpace(out)
	if len(out) <= outputLimit {
		return out
	}

	// Cut on a line boundary so the tail starts with a whole line.
	tail := out[len(out)-outputLimit:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	dropped := out[:len(out)-len(tail)]

	var report strings.Builder
	for _, line := range failureLines(dropped) {
		report.WriteString(line + "\n")
	}
	report.WriteString("... (truncated)\n")
	report.WriteString(tail)
	return report.String()
}

// failureLinesLimit caps the rescued lines; a run with hundreds of failures would otherwise
// reproduce the output the truncation exists to avoid.
const failureLinesLimit = 30

// failureLine matches how the tools in a pipeline mark a line as the problem: the Symfony
// console's [ERROR] blocks and status columns, phpstan and phpunit failures, PHP fatals.
var failureLine = regexp.MustCompile(`(?i)\b(error|errors|fail|failed|failure|failures|fatal|not fulfilled|unfulfilled)\b`)

func failureLines(text string) []string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || !failureLine.MatchString(line) {
			continue
		}
		kept = append(kept, line)
		if len(kept) == failureLinesLimit {
			break
		}
	}
	return kept
}

func indent(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

// stripTraceparent removes the OpenTelemetry id Dagger appends to its messages; it is noise for
// anyone who is not debugging the engine itself.
func stripTraceparent(message string) string {
	start := strings.Index(message, "[traceparent:")
	if start == -1 {
		return message
	}
	end := strings.Index(message[start:], "]")
	if end == -1 {
		return strings.TrimSpace(message[:start])
	}
	return strings.TrimSpace(message[:start] + message[start+end+1:])
}
