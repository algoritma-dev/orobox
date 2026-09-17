package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// withTerminal makes the update check believe stdout is a terminal for the duration of a test.
// Without it the tests inherit the test binary's own stdout, which is a pipe under `go test`.
func withTerminal(t *testing.T, isTerminal bool) {
	t.Helper()
	prev := updateCheckStdoutIsTerminal
	updateCheckStdoutIsTerminal = func() bool { return isTerminal }
	t.Cleanup(func() { updateCheckStdoutIsTerminal = prev })
}

func TestUpdateCheckEnabled(t *testing.T) {
	t.Run("EnabledOnATerminalForAnOrdinaryCommand", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")

		if !updateCheckEnabled("up") {
			t.Error("expected the check to be enabled for up on a terminal")
		}
	})

	t.Run("DisabledInAgentMode", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")
		output.SetAgent(true)
		t.Cleanup(func() { output.SetAgent(false) })

		if updateCheckEnabled("up") {
			t.Error("expected no check in agent mode: the notice would pollute the payload")
		}
	})

	t.Run("DisabledInCI", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "true")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")

		if updateCheckEnabled("up") {
			t.Error("expected no check when CI is set")
		}
	})

	t.Run("DisabledByOroNoUpdateCheck", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "1")

		if updateCheckEnabled("up") {
			t.Error("expected no check when ORO_NO_UPDATE_CHECK is set")
		}
	})

	t.Run("DisabledOffATerminal", func(t *testing.T) {
		withTerminal(t, false)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")

		if updateCheckEnabled("up") {
			t.Error("expected no check when stdout is not a terminal")
		}
	})

	t.Run("DisabledForTheCommandsThatReportVersionsThemselves", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")

		for _, name := range []string{"self-update", "version", "help", "completion"} {
			if updateCheckEnabled(name) {
				t.Errorf("expected no check for %s", name)
			}
		}
	})
}

func TestUpdateNoticeMessage(t *testing.T) {
	t.Run("EmptyWhenTheLatestVersionIsTheCurrentOne", func(t *testing.T) {
		if got := updateNoticeMessage("1.0.0-rc33", "1.0.0-rc33"); got != "" {
			t.Errorf("expected no message for the same version, got %q", got)
		}
	})

	t.Run("EmptyWhenTheLatestVersionIsUnknown", func(t *testing.T) {
		if got := updateNoticeMessage("", "1.0.0-rc33"); got != "" {
			t.Errorf("expected no message when the check produced nothing, got %q", got)
		}
	})

	t.Run("NamesBothVersionsAndTheCommandToRun", func(t *testing.T) {
		got := updateNoticeMessage("1.0.0-rc34", "1.0.0-rc33")
		want := "New version available: 1.0.0-rc34 (current 1.0.0-rc33). Run: orobox self-update"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// withUpdateCache points the check at a cache file inside a temporary directory and, when
// contents is non-empty, seeds the file with it. It returns the file path.
func withUpdateCache(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "update-check.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("could not seed the cache file: %v", err)
		}
	}

	prev := updateCachePath
	updateCachePath = func() string { return path }
	t.Cleanup(func() { updateCachePath = prev })

	return path
}

// withFetch replaces the network call. Tests that must never reach it pass a function failing the
// test, which is the only way to prove the cache was used.
func withFetch(t *testing.T, fn func() (string, error)) {
	t.Helper()
	prev := fetchLatestVersion
	fetchLatestVersion = fn
	t.Cleanup(func() { fetchLatestVersion = prev })
}

func seededCache(t *testing.T, age time.Duration, version string) string {
	t.Helper()

	data, err := json.Marshal(updateCache{
		LastCheck:     time.Now().Add(-age),
		LatestVersion: version,
	})
	if err != nil {
		t.Fatalf("could not build the cache contents: %v", err)
	}
	return string(data)
}

func TestRunUpdateCheck(t *testing.T) {
	t.Run("UsesAFreshCacheWithoutCallingGitHub", func(t *testing.T) {
		withUpdateCache(t, seededCache(t, time.Hour, "1.0.0-rc99"))
		withFetch(t, func() (string, error) {
			t.Error("expected no network call while the cache is fresh")
			return "", nil
		})

		if got := runUpdateCheck(); got != "1.0.0-rc99" {
			t.Errorf("got %q, want the cached 1.0.0-rc99", got)
		}
	})

	t.Run("RefreshesAStaleCache", func(t *testing.T) {
		path := withUpdateCache(t, seededCache(t, 25*time.Hour, "1.0.0-rc98"))
		withFetch(t, func() (string, error) { return "1.0.0-rc99", nil })

		if got := runUpdateCheck(); got != "1.0.0-rc99" {
			t.Errorf("got %q, want the fetched 1.0.0-rc99", got)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read the cache back: %v", err)
		}
		var c updateCache
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("cache is not valid JSON: %v", err)
		}
		if c.LatestVersion != "1.0.0-rc99" {
			t.Errorf("cached version is %q, want 1.0.0-rc99", c.LatestVersion)
		}
		if time.Since(c.LastCheck) > time.Minute {
			t.Errorf("cache timestamp was not refreshed: %v", c.LastCheck)
		}
	})

	t.Run("FetchesWhenThereIsNoCacheYet", func(t *testing.T) {
		path := withUpdateCache(t, "")
		withFetch(t, func() (string, error) { return "1.0.0-rc99", nil })

		if got := runUpdateCheck(); got != "1.0.0-rc99" {
			t.Errorf("got %q, want 1.0.0-rc99", got)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected the cache file to be written: %v", err)
		}
	})

	t.Run("TreatsACorruptCacheAsMissing", func(t *testing.T) {
		withUpdateCache(t, "not json at all")
		withFetch(t, func() (string, error) { return "1.0.0-rc99", nil })

		if got := runUpdateCheck(); got != "1.0.0-rc99" {
			t.Errorf("got %q, want the fetched 1.0.0-rc99", got)
		}
	})

	t.Run("StaysSilentAndKeepsNoCacheWhenTheFetchFails", func(t *testing.T) {
		path := withUpdateCache(t, "")
		withFetch(t, func() (string, error) { return "", errors.New("no network") })

		if got := runUpdateCheck(); got != "" {
			t.Errorf("got %q, want an empty result on a failed check", got)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("expected no cache file after a failed check, so the next run retries")
		}
	})
}

func TestStartUpdateCheckDoesNothingWhenDisabled(t *testing.T) {
	withTerminal(t, false)
	t.Setenv("CI", "")
	t.Setenv("ORO_NO_UPDATE_CHECK", "")
	withUpdateCache(t, "")
	withFetch(t, func() (string, error) {
		t.Error("expected no check at all when the conditions are not met")
		return "", nil
	})

	startUpdateCheck("up")
	t.Cleanup(func() { updateCheckResult = nil })

	if out := captureNotice(t); out != "" {
		t.Errorf("expected nothing printed, got %q", out)
	}
}

// captureNotice runs printUpdateNotice with the print helpers redirected and returns what it wrote.
func captureNotice(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	restore := utils.SetWriter(&buf)
	defer restore()

	printUpdateNotice()

	return buf.String()
}

func TestPrintUpdateNotice(t *testing.T) {
	t.Run("ReportsANewerRelease", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")
		withUpdateCache(t, seededCache(t, time.Hour, "1.0.0-rc99"))
		withFetch(t, func() (string, error) { return "", errors.New("must not be called") })

		startUpdateCheck("up")
		t.Cleanup(func() { updateCheckResult = nil })

		out := captureNotice(t)
		if !strings.Contains(out, "1.0.0-rc99") || !strings.Contains(out, "orobox self-update") {
			t.Errorf("expected the notice to name the release and the command, got %q", out)
		}
	})

	t.Run("SaysNothingWhenTheReleaseIsTheRunningVersion", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")
		withUpdateCache(t, seededCache(t, time.Hour, Version))
		withFetch(t, func() (string, error) { return "", errors.New("must not be called") })

		startUpdateCheck("up")
		t.Cleanup(func() { updateCheckResult = nil })

		if out := captureNotice(t); out != "" {
			t.Errorf("expected nothing printed, got %q", out)
		}
	})

	t.Run("SaysNothingWhenNoCheckWasStarted", func(t *testing.T) {
		updateCheckResult = nil

		if out := captureNotice(t); out != "" {
			t.Errorf("expected nothing printed, got %q", out)
		}
	})

	t.Run("GivesUpOnASlowCheckInsteadOfHoldingTheCommand", func(t *testing.T) {
		withTerminal(t, true)
		t.Setenv("CI", "")
		t.Setenv("ORO_NO_UPDATE_CHECK", "")
		withUpdateCache(t, "")

		release := make(chan struct{})
		withFetch(t, func() (string, error) {
			<-release
			return "1.0.0-rc99", nil
		})

		prevWait := updateNoticeWait
		updateNoticeWait = 20 * time.Millisecond
		t.Cleanup(func() { updateNoticeWait = prevWait })

		startUpdateCheck("up")

		// The abandoned goroutine outlives the assertions, so it has to finish before the cleanups
		// restore the hooks it still reads. Receiving from its channel is what proves it returned.
		ch := updateCheckResult
		t.Cleanup(func() {
			close(release)
			<-ch
			updateCheckResult = nil
		})

		start := time.Now()
		out := captureNotice(t)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("printUpdateNotice waited %v for a slow check", elapsed)
		}
		if out != "" {
			t.Errorf("expected nothing printed for a check that did not finish, got %q", out)
		}
	})
}

// TestRootStartsTheUpdateCheck pins the wiring: without this the check exists but never runs.
func TestRootStartsTheUpdateCheck(t *testing.T) {
	withTerminal(t, true)
	t.Setenv("CI", "")
	t.Setenv("ORO_NO_UPDATE_CHECK", "")
	withUpdateCache(t, seededCache(t, time.Hour, "1.0.0-rc99"))
	withFetch(t, func() (string, error) { return "", errors.New("must not be called") })

	updateCheckResult = nil
	t.Cleanup(func() { updateCheckResult = nil })

	rootCmd.PersistentPreRun(&cobra.Command{Use: "up"}, nil)

	ch := updateCheckResult
	if ch == nil {
		t.Fatal("expected the root command to start the update check")
	}

	// Drain it before the cleanups restore the hooks the goroutine reads.
	<-ch
}
