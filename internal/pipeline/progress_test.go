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

func TestElapsedSwitchesToMinutes(t *testing.T) {
	if got := elapsed(time.Now().Add(-90 * time.Second)); got != "1m30s" {
		t.Errorf("elapsed = %q, want 1m30s", got)
	}
	if got := elapsed(time.Now().Add(-5 * time.Second)); got != "5s" {
		t.Errorf("elapsed = %q, want 5s", got)
	}
}
