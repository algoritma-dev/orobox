package pipeline

import (
	"strings"
	"testing"
	"time"
)

// clockStart is the fixed instant the reader tests hand to drain, so lastSeen is comparable
// without touching the wall clock.
var clockStart = time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)

// streamBurstLines is more output than one poll would ever have printed under the old tail cap,
// which is the point: a burst is streamed whole.
const streamBurstLines = 24

// installLine is what the engine prints when it starts the QA warmup command. The arguments are
// rendered on one line, with the script's newlines escaped.
const installLine = `12  : Container.withExec(args: ["bash", "-o", "pipefail", "-c", "exec 2>&1\nset -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint\nphp bin/console oro:install"]): Container!`

const testsLine = `13  : Container.withExec(args: ["bash", "-o", "pipefail", "-c", "exec 2>&1\nvendor/bin/phpunit --testsuite unit"]): Container!`

const releaseLine = `14  : Container.withExec(args: ["bash", "-o", "pipefail", "-c", "exec 2>&1\nphp vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v"]): Container!`

func TestParseEngineLineSplitsOutputFromDescription(t *testing.T) {
	id, text, isOutput := parseEngineLine("\x1b[95m12  : \x1b[0m\x1b[90m[3.4s] \x1b[0m\x1b[90m|\x1b[0m Installing Oro")
	if id != "12" || text != "Installing Oro" || !isOutput {
		t.Errorf("parseEngineLine = (%q, %q, %v), want the span's output line", id, text, isOutput)
	}

	// Nested spans are indented before the elapsed time.
	if id, text, isOutput := parseEngineLine("2   : ┆ [0.0s] | Client: Docker Engine"); id != "2" || text != "Client: Docker Engine" || !isOutput {
		t.Errorf("parseEngineLine = (%q, %q, %v), want the nested span's output line", id, text, isOutput)
	}

	if id, text, isOutput := parseEngineLine(installLine); id != "12" || isOutput || !strings.Contains(text, "oro:install") {
		t.Errorf("parseEngineLine = (%q, %q, %v), want the operation description", id, text, isOutput)
	}

	if id, _, _ := parseEngineLine("Establishing connection to Engine... OK!"); id != "" {
		t.Error("a line without a span id is not progress output")
	}
}

func TestEngineWatchGivesEachCommandItsOwnOutput(t *testing.T) {
	log := newLogBuffer(nil)
	watch := newEngineWatch(log)

	qa := watch.follow("set -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint\nphp bin/console oro:install")
	tests := watch.follow("vendor/bin/phpunit --testsuite unit")

	log.Write([]byte(installLine + "\n12  : [1.0s] | Installing Oro\n" +
		testsLine + "\n13  : [1.1s] | PHPUnit 10\n12  : [2.0s] | Warming the cache\n"))

	if got := qa.drain(clockStart); len(got) != 2 || got[0] != "Installing Oro" || got[1] != "Warming the cache" {
		t.Errorf("qa output = %q, want only the install command's lines", got)
	}
	if got := tests.drain(clockStart); len(got) != 1 || got[0] != "PHPUnit 10" {
		t.Errorf("test output = %q, want only the test command's lines", got)
	}

	// A poll with nothing new since the last one prints nothing at all.
	if got := qa.drain(clockStart); len(got) != 0 {
		t.Errorf("qa output = %q, want nothing repeated", got)
	}

	log.Write([]byte("12  : [3.0s] | Done\n"))
	if got := qa.drain(clockStart); len(got) != 1 || got[0] != "Done" {
		t.Errorf("qa output = %q, want the line written since the last poll", got)
	}
}

func TestDrainReturnsEverySeenLine(t *testing.T) {
	log := newLogBuffer(nil)
	watch := newEngineWatch(log)
	release := watch.follow("php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")

	var engine strings.Builder
	engine.WriteString(releaseLine + "\n")
	for i := 0; i < streamBurstLines; i++ {
		engine.WriteString("14  : [1.0s] | line\n")
	}
	log.Write([]byte(engine.String()))

	if got := release.drain(clockStart); len(got) != streamBurstLines {
		t.Errorf("drained %d lines, want %d: streaming must not be capped", len(got), streamBurstLines)
	}
	if got := release.drain(clockStart); len(got) != 0 {
		t.Errorf("drained %q, want nothing repeated", got)
	}
}

func TestLastSeenTracksTheMostRecentLine(t *testing.T) {
	log := newLogBuffer(nil)
	watch := newEngineWatch(log)
	release := watch.follow("php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")

	if _, _, ok := release.lastSeen(); ok {
		t.Error("lastSeen must report nothing before the first line arrives")
	}

	log.Write([]byte(releaseLine + "\n14  : [1.0s] | task oro:update\n14  : [2.0s] | run oro:platform:update\n"))
	release.drain(clockStart)

	line, at, ok := release.lastSeen()
	if !ok || line != "run oro:platform:update" || !at.Equal(clockStart) {
		t.Errorf("lastSeen = (%q, %v, %v), want the last drained line at the drain's clock", line, at, ok)
	}

	// A drain that finds nothing leaves the last line and its timestamp alone: that is what
	// makes "no output for 3m" true rather than "no output for 1s".
	release.drain(clockStart.Add(time.Minute))
	if _, at, _ := release.lastSeen(); !at.Equal(clockStart) {
		t.Errorf("lastSeen time = %v, want it unchanged by an empty drain", at)
	}
}

func TestCommandKeyStopsBeforeEscapedCharacters(t *testing.T) {
	// The engine renders the arguments as a quoted string, so a quote in the command appears
	// escaped there and would never match.
	if got := commandKey(`php bin/console doctrine:query:sql --env=test "DROP SCHEMA"`); got != "php bin/console doctrine:query:sql --env=test" {
		t.Errorf("commandKey = %q, want everything before the quote", got)
	}

	if got := commandKey("set -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint"); got != "stamp=/var/www/oro/var/cache/.orobox-qa-fingerprint" {
		t.Errorf("commandKey = %q, want the first meaningful line", got)
	}

	// Nothing distinctive enough to tell two commands apart.
	if got := commandKey("ls"); got != "" {
		t.Errorf("commandKey = %q, want no key for a command too short to identify", got)
	}
}

// cachedExecLines are dagger 0.21.8's rendering of an exec served from the cache: the arguments
// first, then the closing status. The status line names the operation, not the command, so letting
// it become the span's description would leave the span matchable by nothing.
const cachedExecLines = `18  : ┆ withExec bash -o pipefail -c 'exec 2>&1\ncomposer install --no-dev --no-interaction'
18  : ┆ Container.withExec CACHED [0.0s]
`

func TestEngineWatchReadsTheClosingStatusOfASpan(t *testing.T) {
	log := newLogBuffer(nil)
	watch := newEngineWatch(log)
	reader := watch.follow("composer install --no-dev --no-interaction")

	// Nothing has been written yet: an unbound span is not something to wait for.
	if status, bound := reader.status(); status != "" || bound {
		t.Errorf("status() = (%q, %v), want the span not found yet", status, bound)
	}

	log.Write([]byte(cachedExecLines))

	status, bound := reader.status()
	if !bound || status != engineStatusCached {
		t.Errorf("status() = (%q, %v), want the span reported as cached", status, bound)
	}
	// The arguments line has to survive as the description, or nothing would have bound the span.
	if got := watch.spans["18"].description; !strings.Contains(got, "composer install") {
		t.Errorf("description = %q, want the rendered arguments, not the closing line", got)
	}
	if got := reader.drain(clockStart); len(got) != 0 {
		t.Errorf("drain() = %q, want nothing: a cached exec writes no output line", got)
	}
}

func TestEngineWatchDistinguishesAFinishedSpanFromACachedOne(t *testing.T) {
	log := newLogBuffer(nil)
	watch := newEngineWatch(log)
	reader := watch.follow("vendor/bin/phpunit --testsuite unit")

	log.Write([]byte(testsLine + "\n13  : ┆ Container.withExec DONE [187.4s]\n"))

	if status, bound := reader.status(); !bound || status == engineStatusCached {
		t.Errorf("status() = (%q, %v), want a span that ran", status, bound)
	}
}

func TestEngineSpanStatusMatchesOnlyAClosingLine(t *testing.T) {
	for _, line := range []string{
		"Container.withExec CACHED [0.0s]",
		"Container.withExec DONE [1.5s]",
		"Container.withExec ERROR [0.3s]",
		// A session the engine closes without timing it still says what became of it.
		"Container.withExec CACHED",
	} {
		if !engineSpanStatus.MatchString(line) {
			t.Errorf("engineSpanStatus did not match the closing line %q", line)
		}
	}

	for _, line := range []string{
		// The arguments of a command that happens to mention one of the words.
		`withExec bash -o pipefail -c 'echo DONE > /tmp/flag'`,
		`withExec bash -o pipefail -c 'php bin/console cache:warmup'`,
	} {
		if engineSpanStatus.MatchString(line) {
			t.Errorf("engineSpanStatus matched %q, which is a description", line)
		}
	}
}
