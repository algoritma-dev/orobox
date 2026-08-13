package pipeline

import (
	"regexp"
	"strings"
	"sync"
)

// The Dagger SDK hands a command's own output over only when the command exits, so a step that
// installs Oro is silent for as long as it takes. Its live output does exist, in the engine's
// plain progress stream: every line there carries the span it belongs to, either as the
// description of the operation or as one line the operation printed.
//
//	42  : Container.withExec(args: ["bash", "-o", "pipefail", "-c", "…install…"]): Container!
//	42  : [12.3s] | Installing Oro …
//
// engineWatch turns that stream back into per-command output: it remembers which span each
// withExec belongs to, so a heartbeat quotes the running command's own lines and not the ones
// the concurrently running step is printing at the same time.
type engineWatch struct {
	mu     sync.Mutex
	log    *logBuffer
	cursor int64
	spans  map[string]*engineSpan
	// readers wait for the span that matches their command; a command is announced only when the
	// engine starts running it, which is after the heartbeat that follows it began.
	readers []*engineReader
}

// engineSpan is what has been seen of one engine operation.
type engineSpan struct {
	// description is the operation line, which for an exec contains the command arguments.
	description string
	lines       []string
}

// spanLineLimit caps what is kept per span. A heartbeat prints a handful of lines and a long
// install writes tens of thousands, so only the recent tail is worth holding on to.
const spanLineLimit = 200

// engineReader follows one command's output. Each running command has its own, so two
// concurrent steps never consume each other's lines.
type engineReader struct {
	watch *engineWatch
	// key identifies the command inside a withExec description.
	key    string
	spanID string
	// seen counts the lines of this span already returned, in the span's own numbering.
	seen int
}

func newEngineWatch(log *logBuffer) *engineWatch {
	return &engineWatch{log: log, cursor: log.Offset(), spans: map[string]*engineSpan{}}
}

// follow returns a reader for the output of command, which the engine has usually not started
// yet: the span is bound on the first read that finds it.
func (w *engineWatch) follow(command string) *engineReader {
	r := &engineReader{watch: w, key: commandKey(command)}
	if r.key == "" {
		return r
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.readers = append(w.readers, r)
	return r
}

// next returns up to limit new lines of this command's output, oldest hidden. A nil reader
// returns nothing, so a reporter without an engine log needs no special case.
func (r *engineReader) next(limit int) []string {
	if r == nil || r.key == "" {
		return nil
	}

	r.watch.mu.Lock()
	defer r.watch.mu.Unlock()

	r.watch.consume()
	if r.spanID == "" {
		r.spanID = r.watch.findSpan(r.key)
		if r.spanID == "" {
			return nil
		}
	}

	span := r.watch.spans[r.spanID]
	if span == nil || len(span.lines) <= r.seen {
		return nil
	}
	lines := span.lines[r.seen:]
	r.seen = len(span.lines)
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return append([]string(nil), lines...)
}

// consume folds everything written to the engine log since the last call into the spans. The
// caller holds the lock.
func (w *engineWatch) consume() {
	raw, cursor := w.log.Since(w.cursor)
	w.cursor = cursor

	for _, line := range raw {
		id, text, isOutput := parseEngineLine(line)
		if id == "" {
			continue
		}

		span := w.spans[id]
		if span == nil {
			span = &engineSpan{}
			w.spans[id] = span
		}
		if !isOutput {
			// The description is repeated every time the engine reprints the span; keeping the
			// first one is enough to match a command against it.
			if span.description == "" {
				span.description = text
			}
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		span.lines = append(span.lines, text)
		if len(span.lines) > spanLineLimit {
			dropped := len(span.lines) - spanLineLimit
			span.lines = span.lines[dropped:]
			w.rebase(id, dropped)
		}
	}
}

// rebase keeps the readers of a trimmed span pointing at the same place in it.
func (w *engineWatch) rebase(spanID string, dropped int) {
	for _, r := range w.readers {
		if r.spanID != spanID {
			continue
		}
		if r.seen -= dropped; r.seen < 0 {
			r.seen = 0
		}
	}
}

// findSpan returns the span whose description contains key and that no other reader has taken.
// The caller holds the lock.
func (w *engineWatch) findSpan(key string) string {
	taken := map[string]bool{}
	for _, r := range w.readers {
		if r.spanID != "" {
			taken[r.spanID] = true
		}
	}

	for id, span := range w.spans {
		if !taken[id] && strings.Contains(span.description, key) {
			return id
		}
	}
	return ""
}

var (
	ansiCodes = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	// A plain-progress line is a span id, then either the operation or one line of its output.
	engineSpanLine = regexp.MustCompile(`^\s*(\d+)\s*:\s*(.*)$`)
	// Output lines carry the elapsed time and a pipe before the text the command wrote.
	engineOutputLine = regexp.MustCompile(`^[┆|\s]*\[[\d.]+m?s\]\s*\|\s?(.*)$`)
)

// parseEngineLine splits one line of plain progress output into the span it belongs to and its
// text, saying whether the text is output of the operation or its description.
func parseEngineLine(line string) (id, text string, isOutput bool) {
	line = ansiCodes.ReplaceAllString(line, "")
	// A progress bar rewrites the same line with \r; only the last state is meaningful.
	if idx := strings.LastIndex(line, "\r"); idx >= 0 {
		line = line[idx+1:]
	}

	match := engineSpanLine.FindStringSubmatch(line)
	if match == nil {
		return "", "", false
	}

	rest := match[2]
	if out := engineOutputLine.FindStringSubmatch(rest); out != nil {
		return match[1], strings.TrimRight(out[1], " \t"), true
	}
	return match[1], strings.TrimLeft(rest, "┆| \t"), false
}

// commandKey is the part of a command that identifies it inside the engine's rendering of the
// exec arguments. The rendering escapes quotes and newlines, so the key stops at the first
// character whose escaped form would no longer match the command as written.
func commandKey(command string) string {
	key := label(command)
	if idx := strings.IndexAny(key, `"'\`); idx >= 0 {
		key = key[:idx]
	}
	key = strings.TrimSpace(key)

	const keyLimit = 60
	if len([]rune(key)) > keyLimit {
		key = string([]rune(key)[:keyLimit])
	}
	// Too short a key would match any exec; the caller then reports elapsed time only.
	if len(key) < 8 {
		return ""
	}
	return key
}
