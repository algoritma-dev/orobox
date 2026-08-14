package pipeline

import (
	"testing"
	"time"
)

var trackerStart = time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)

func TestParseTaskLineRecognisesDeployersAnnouncement(t *testing.T) {
	if name, ok := parseTaskLine("task oro:update"); !ok || name != "oro:update" {
		t.Errorf("parseTaskLine = (%q, %v), want the task name", name, ok)
	}

	// Symfony renders the label with colour when the output is decorated.
	if name, ok := parseTaskLine("\x1b[36;1mtask\x1b[39;22m deploy:release"); !ok || name != "deploy:release" {
		t.Errorf("parseTaskLine = (%q, %v), want the name behind the colour codes", name, ok)
	}

	if name, ok := parseTaskLine("::group::task oro:cache_warmup"); !ok || name != "oro:cache_warmup" {
		t.Errorf("parseTaskLine = (%q, %v), want the GitHub-grouped name", name, ok)
	}

	for _, line := range []string{
		"",
		"[pcmu000422] run /usr/bin/php bin/console oro:platform:update --force",
		"[pcmu000422] info New migrations detected: running oro:platform:update.",
		"task",
	} {
		if name, ok := parseTaskLine(line); ok {
			t.Errorf("parseTaskLine(%q) = %q, want no match", line, name)
		}
	}
}

func TestTrackerNamesTheRunningTask(t *testing.T) {
	d := newDeployerTracker()
	if got := d.currentTask(); got != "" {
		t.Errorf("currentTask = %q, want nothing before the first task line", got)
	}

	d.observe("task deploy:release", trackerStart)
	if got := d.currentTask(); got != "deploy:release" {
		t.Errorf("currentTask = %q, want deploy:release", got)
	}

	d.observe("[host] some output", trackerStart.Add(time.Second))
	if got := d.currentTask(); got != "deploy:release" {
		t.Errorf("currentTask = %q, want the task to survive an output line", got)
	}

	d.observe("task oro:update", trackerStart.Add(2*time.Second))
	if got := d.currentTask(); got != "oro:update" {
		t.Errorf("currentTask = %q, want the new task", got)
	}
}

func TestSummaryOrdersTasksByDuration(t *testing.T) {
	d := newDeployerTracker()
	d.observe("task deploy:release", trackerStart)
	d.observe("task oro:update", trackerStart.Add(8*time.Second))
	d.observe("task oro:cache_warmup", trackerStart.Add(8*time.Second+6*time.Minute+12*time.Second))

	got := d.summary(trackerStart.Add(8*time.Second + 6*time.Minute + 12*time.Second + 3*time.Minute + 40*time.Second))
	want := "oro:update 6m12s · oro:cache_warmup 3m40s · deploy:release 8s"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestSummaryTruncatesToTheLongestTasks(t *testing.T) {
	d := newDeployerTracker()
	names := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7"}
	at := trackerStart
	// Descending durations, so the tail that gets bucketed is the last two.
	for i, name := range names {
		d.observe("task "+name, at)
		at = at.Add(time.Duration(len(names)-i) * time.Minute)
	}

	got := d.summary(at)
	want := "t1 7m00s · t2 6m00s · t3 5m00s · t4 4m00s · t5 3m00s · others 3m00s"
	if got != want {
		t.Errorf("summary = %q, want the five longest plus a bucket", got)
	}
}

func TestSummaryOfATrackerThatSawNothing(t *testing.T) {
	if got := newDeployerTracker().summary(trackerStart); got != "" {
		t.Errorf("summary = %q, want nothing to print", got)
	}
}

func TestHumanDurationSwitchesToMinutes(t *testing.T) {
	if got := humanDuration(45 * time.Second); got != "45s" {
		t.Errorf("humanDuration = %q, want 45s", got)
	}
	if got := humanDuration(6*time.Minute + 12*time.Second); got != "6m12s" {
		t.Errorf("humanDuration = %q, want 6m12s", got)
	}
	// The seconds are zero-padded, so a minute on the nose reads 1m00s and not 1m0s.
	if got := humanDuration(time.Minute + 400*time.Millisecond); got != "1m00s" {
		t.Errorf("humanDuration = %q, want 1m00s", got)
	}
	if got := humanDuration(time.Minute + 500*time.Millisecond); got != "1m01s" {
		t.Errorf("humanDuration = %q, want the half second rounded up", got)
	}
}
