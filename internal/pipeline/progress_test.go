package pipeline

import (
	"strings"
	"testing"
	"time"
)

func TestCleanOutputCollapsesRedrawsAndBlanks(t *testing.T) {
	raw := "\n\nDownloading: 10%\rDownloading: 100%\n\n\n  done  \n\n"

	lines := cleanOutput(raw)

	want := []string{"Downloading: 100%", "", "  done"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCleanOutputOfEmptyCommand(t *testing.T) {
	if lines := cleanOutput("\n \n"); len(lines) != 0 {
		t.Errorf("lines = %q, want none", lines)
	}
}

func TestBodyKeepsTheTailUnlessVerbose(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < outputTailLines+5; i++ {
		raw.WriteString("line\n")
	}

	body := newReporter(&strings.Builder{}, false, nil).body(raw.String())
	if !strings.Contains(body, "5 earlier lines hidden") {
		t.Errorf("the hidden-line notice is missing:\n%s", body)
	}
	if got := strings.Count(body, "    line"); got != outputTailLines {
		t.Errorf("printed %d lines, want %d", got, outputTailLines)
	}

	verbose := newReporter(&strings.Builder{}, true, nil).body(raw.String())
	if strings.Contains(verbose, "hidden") {
		t.Errorf("--debug should print everything:\n%s", verbose)
	}
	if got := strings.Count(verbose, "    line"); got != outputTailLines+5 {
		t.Errorf("printed %d lines, want %d", got, outputTailLines+5)
	}
}

func TestLabelFitsOneLine(t *testing.T) {
	if got := label("composer  install   --no-dev"); got != "composer install --no-dev" {
		t.Errorf("label = %q, want the command on one line", got)
	}

	// A script's first real line identifies it; the shell bookkeeping around it does not.
	if got := label("set -e\n[ -f phpstan.neon ] || exit 0\nsed -i ..."); got != "[ -f phpstan.neon ] || exit 0" {
		t.Errorf("label = %q, want the first meaningful line", got)
	}

	// A script whose first line says nothing about the ten minutes after it titles itself.
	script := "# Preparing the test database\nset -e\napk add --no-cache postgresql-client\nphp bin/console oro:install"
	if got := label(script); got != "Preparing the test database" {
		t.Errorf("label = %q, want the script's own title", got)
	}
	// The engine renders the arguments, so the span is still matched on a line really run.
	if got := commandKey(script); got != "apk add --no-cache postgresql-client" {
		t.Errorf("commandKey = %q, want the first executable line", got)
	}

	// A comment further down documents that line, not the script.
	if got := label("composer install\n# dumps the autoloader\ncomposer dump-autoload"); got != "composer install" {
		t.Errorf("label = %q, want the first line, not the comment below it", got)
	}

	long := label(strings.Repeat("x", commandLabelLimit+50))
	if len([]rune(long)) != commandLabelLimit {
		t.Errorf("label length = %d, want %d", len([]rune(long)), commandLabelLimit)
	}
}

func TestReporterWritesOneBlockPerCommand(t *testing.T) {
	var out strings.Builder
	r := newReporter(&out, false, nil)

	r.start("qa", "bin/phpstan analyze").ok("[OK] No errors\n")
	r.start("test", "bin/simple-phpunit").fail()

	text := out.String()
	for _, want := range []string{"[qa]", "bin/phpstan analyze", "    [OK] No errors", "[test]", "✘"} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
}

func TestHeartbeatStaysQuietWithDebug(t *testing.T) {
	log := newLogBuffer(nil)
	r := newReporter(&strings.Builder{}, true, log)

	if task := r.start("qa", "php bin/console oro:install --env=test"); task.reader != nil {
		t.Error("a --debug run streams the engine log already; the heartbeat must not quote it too")
	}
}

func TestReleaseStreamsEveryLine(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start(releaseStepName, "php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")
	// The stream goroutine polls once a second; this test drives pollStream itself.
	task.finish()

	if !task.stream {
		t.Fatal("the release step must stream")
	}

	var engine strings.Builder
	engine.WriteString(releaseLine + "\n")
	engine.WriteString("14  : [1.0s] | task oro:update\n")
	for i := 0; i < streamBurstLines; i++ {
		engine.WriteString("14  : [1.0s] | migration line\n")
	}
	log.Write([]byte(engine.String()))

	out.Reset()
	if !task.pollStream(now) {
		t.Fatal("pollStream should report the lines it printed")
	}
	if got := strings.Count(out.String(), "    migration line"); got != streamBurstLines {
		t.Errorf("printed %d lines, want %d: streaming must print the whole burst", got, streamBurstLines)
	}
	if task.tracker.currentTask() != "oro:update" {
		t.Errorf("currentTask = %q, want the streamed task line to have been tracked", task.tracker.currentTask())
	}
}

func TestReleaseClosesWithTheTaskTimings(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start(releaseStepName, "php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")
	task.finish()
	log.Write([]byte(releaseLine + "\n14  : [1.0s] | task oro:update\n"))
	task.pollStream(now)

	r.now = func() time.Time { return now.Add(6*time.Minute + 12*time.Second) }
	out.Reset()
	task.ok("run /usr/bin/php bin/console oro:platform:update --force\n")

	text := out.String()
	if !strings.Contains(text, "oro:update 6m12s") {
		t.Errorf("closing block = %q, want the task timing", text)
	}
	// Everything the release printed was streamed already; printing the tail again would repeat it.
	if strings.Contains(text, "oro:platform:update --force") {
		t.Errorf("closing block = %q, want no duplicated output tail", text)
	}
}

func TestReleaseUnderDebugDoesNotStream(t *testing.T) {
	log := newLogBuffer(nil)
	r := newReporter(&strings.Builder{}, true, log)

	task := r.start(releaseStepName, "php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")
	task.finish()

	if task.stream {
		t.Error("--debug streams the engine log already; the release step must not print it twice")
	}
}

func TestEveryStepStreamsItsOutputExactlyOnce(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start("qa", "set -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint\nphp bin/console oro:install")
	// The stream goroutine polls once a second; this test drives the polls itself.
	task.finish()
	if !task.stream {
		t.Fatal("every step streams: a build that says nothing for ten minutes is what this is for")
	}

	log.Write([]byte(installLine + "\n12  : [1.0s] | Installing Oro\n"))
	out.Reset()
	// Whatever the last poll missed is drained by the closing block, so the line appears there —
	// and printing the captured stdout as well would show it twice.
	task.ok("Installing Oro\n")

	if got := strings.Count(out.String(), "Installing Oro"); got != 1 {
		t.Errorf("printed the line %d times, want once:\n%s", got, out.String())
	}
}

func TestACommandWithNoMatchedSpanStillPrintsItsOutput(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	// "ls" carries nothing distinctive enough to bind a span, so nothing is ever streamed for it.
	task := r.start("qa", "ls")
	task.finish()
	out.Reset()
	task.ok("composer.json\n")

	if !strings.Contains(out.String(), "    composer.json") {
		t.Errorf("closing block = %q, want the captured output when nothing was streamed", out.String())
	}
}

func TestStreamedOutputIsMarkedWhenTheStepChanges(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	qa := r.start("qa", "set -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint\nphp bin/console oro:install")
	tests := r.start("test", "vendor/bin/phpunit --testsuite unit")
	qa.finish()
	tests.finish()

	log.Write([]byte(installLine + "\n12  : [1.0s] | Installing Oro\n" +
		testsLine + "\n13  : [1.1s] | PHPUnit 10\n"))

	out.Reset()
	qa.pollStream(now)
	tests.pollStream(now)
	// The qa marker is not reprinted: its block was the last thing written before this one.
	qa.pollStream(now)

	text := out.String()
	if !strings.Contains(text, "┄ [test]") {
		t.Errorf("output = %q, want a marker where the streaming step changes", text)
	}
	if got := strings.Count(text, "┄ [qa]"); got != 1 {
		t.Errorf("printed the qa marker %d times, want once: consecutive lines of one step need no marker", got)
	}
}

func TestSilenceLineNamesTheRunningDeployerTask(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	log := newLogBuffer(nil)
	r := newReporter(&strings.Builder{}, false, log)
	r.now = func() time.Time { return now }

	task := r.start("release", "php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v")
	// Stop the beat goroutine first: this test drives the polls itself and would otherwise race
	// it for the reader and the writer.
	task.finish()

	log.Write([]byte(releaseLine + "\n14  : [1.0s] | task oro:update\n" +
		"14  : [2.0s] | [host] run oro:platform:update --force\n"))
	for _, line := range task.reader.drain(now) {
		task.tracker.observe(line, now)
	}

	got := task.silence(now.Add(3*time.Minute + 30*time.Second))
	for _, want := range []string{"[release]", "oro:update", "no output for 3m30s", "last: [host] run oro:platform:update --force"} {
		if !strings.Contains(got, want) {
			t.Errorf("silence line %q is missing %q", got, want)
		}
	}
}

func TestSilenceLineBeforeAnyOutput(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	r := newReporter(&strings.Builder{}, false, nil)
	r.now = func() time.Time { return now }

	task := r.start("qa", "php bin/console oro:install --env=test")
	task.finish()

	got := task.silence(now.Add(45 * time.Second))
	if !strings.Contains(got, "no output yet (running for 45s)") {
		t.Errorf("silence line = %q, want the running-for form", got)
	}
	if strings.Contains(got, "last:") {
		t.Errorf("silence line = %q, want no last-line clause when nothing arrived", got)
	}
}

func TestPollStreamPrintsNothingBeforeTheCommandSpeaks(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start("qa", "set -e\nstamp=/var/www/oro/var/cache/.orobox-qa-fingerprint\nphp bin/console oro:install")
	task.finish()

	out.Reset()
	if task.pollStream(now) {
		t.Error("pollStream reported output before anything was written")
	}
	if out.String() != "" {
		t.Errorf("wrote %q, want nothing: the silence line is the loop's business", out.String())
	}
}

func TestElapsedSwitchesToMinutes(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	if got := elapsed(now.Add(-90*time.Second), now); got != "1m30s" {
		t.Errorf("elapsed = %q, want 1m30s", got)
	}
	if got := elapsed(now.Add(-5*time.Second), now); got != "5s" {
		t.Errorf("elapsed = %q, want 5s", got)
	}
}

func TestHeartbeatBacksOffWhileNothingHappens(t *testing.T) {
	want := []time.Duration{
		30 * time.Second,
		60 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
	}
	for quiet, d := range want {
		if got := heartbeatDelay(quiet); got != d {
			t.Errorf("heartbeatDelay(%d) = %v, want %v", quiet, got, d)
		}
	}

	// Past the end of the schedule the last interval repeats: a mute command keeps proving it
	// is alive without filling the terminal.
	if got := heartbeatDelay(99); got != 5*time.Minute {
		t.Errorf("heartbeatDelay(99) = %v, want the last interval repeated", got)
	}
}

// depsCachedLines are the two lines dagger 0.21.8 prints for a dependency install it serves from
// its cache: the arguments, then the closing status. There is no output line between them, and the
// stdout the SDK returns is nonetheless the full composer run — read from the cache with the rest
// of the exec's result.
const depsCachedLines = `18  : ┆ withExec bash -o pipefail -c 'exec 2>&1\ncomposer install --no-dev --no-interaction --no-progress --no-scripts --no-autoloader'
18  : ┆ Container.withExec CACHED [0.0s]
`

// depsRanLines are the same two lines for an install that really ran. A command can be silent for
// its whole span — tar is, and so is a composer install whose progress is switched off — so the
// closing status is what tells the two apart, not the absence of output.
const depsRanLines = `18  : ┆ withExec bash -o pipefail -c 'exec 2>&1\ncomposer install --no-dev --no-interaction --no-progress --no-scripts --no-autoloader'
18  : ┆ Container.withExec DONE [187.4s]
`

const depsCommand = "composer install --no-dev --no-interaction --no-progress --no-scripts --no-autoloader"

// composerOutput is what the cache holds for the command above, and what a reader mistakes for
// this run's work when it is printed.
const composerOutput = "Installing dependencies from lock file\nGenerating optimized autoload files\n"

func TestACachedCommandNamesTheCacheInsteadOfReplayingItsOutput(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start("deps", depsCommand)
	task.finish()
	log.Write([]byte(depsCachedLines))
	out.Reset()
	task.ok(composerOutput)

	if !strings.Contains(out.String(), "from cache") {
		t.Errorf("closing block = %q, want it to say the result came from the cache", out.String())
	}
	if strings.Contains(out.String(), "Installing dependencies") {
		t.Errorf("closing block = %q, want no replayed output for a command that never ran", out.String())
	}
}

func TestACommandTheEngineRanKeepsItsOutput(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	var out strings.Builder
	log := newLogBuffer(nil)
	r := newReporter(&out, false, log)
	r.now = func() time.Time { return now }

	task := r.start("deps", depsCommand)
	task.finish()
	log.Write([]byte(depsRanLines))
	out.Reset()
	task.ok(composerOutput)

	if strings.Contains(out.String(), "from cache") {
		t.Errorf("closing block = %q, want no cache claim for a command that ran", out.String())
	}
	if !strings.Contains(out.String(), "Installing dependencies") {
		t.Errorf("closing block = %q, want the output of a command that produced none live", out.String())
	}
}
