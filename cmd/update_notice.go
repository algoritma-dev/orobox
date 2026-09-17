package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/term"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// updateCheckInterval is how long a result is reused before GitHub is asked again. A release
// every few days does not deserve a request per command.
const updateCheckInterval = 24 * time.Hour

// updateCheckTimeout bounds the release lookup. It is deliberately far below httpClient's 30s:
// that timeout serves the binary download, where waiting is the point, while nothing here is
// worth making the user wait for.
const updateCheckTimeout = 3 * time.Second

// updateCache is what survives between runs: when the last lookup happened, and what it found.
type updateCache struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
}

// updateCachePath is the cache file's location. Overridable in tests.
//
// It is not config.GetInternalDir(): that directory is per-project, and the newest release of the
// binary is the same fact in every checkout.
var updateCachePath = func() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "orobox", "update-check.json")
}

// fetchLatestVersion returns the tag of the newest release. Overridable in tests.
var fetchLatestVersion = func() (string, error) {
	r, err := getLatestReleaseWith(&http.Client{Timeout: updateCheckTimeout})
	if err != nil {
		return "", err
	}
	return r.TagName, nil
}

// updateCheckStdoutIsTerminal reports whether stdout is a terminal. Overridable in tests, which
// run with stdout on a pipe and would otherwise never see the check enabled.
var updateCheckStdoutIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// updateCheckSkippedCommands are the commands that already speak about versions themselves, or
// that have no human reading their output. Appending the notice there is either redundant or noise.
var updateCheckSkippedCommands = map[string]bool{
	"self-update": true,
	"version":     true,
	"help":        true,
	"completion":  true,
}

// updateCheckEnabled reports whether the running command may look for a newer release.
//
// Every condition here is about not speaking where the line would be read by a machine: agent
// mode and a redirected stdout both mean the output is being parsed, and CI means nobody can act
// on the advice anyway. ORO_NO_UPDATE_CHECK is the explicit opt-out for everyone else.
func updateCheckEnabled(cmdName string) bool {
	if output.Agent() {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if os.Getenv("ORO_NO_UPDATE_CHECK") != "" {
		return false
	}
	if !updateCheckStdoutIsTerminal() {
		return false
	}

	return !updateCheckSkippedCommands[cmdName]
}

// updateNoticeMessage returns the line to print, or an empty string when there is nothing to say.
//
// The comparison is the one self-update already makes: the release tags carry no prefix and match
// Version verbatim, so any difference means a different release.
func updateNoticeMessage(latest, current string) string {
	if latest == "" || latest == current {
		return ""
	}

	return fmt.Sprintf("New version available: %s (current %s). Run: orobox self-update", latest, current)
}

// runUpdateCheck returns the newest known release tag, or an empty string when it cannot be
// established. Every failure is silent: this runs beside a command the user actually asked for,
// and a warning about a failed version check would be noise in front of that command's output.
func runUpdateCheck() string {
	path := updateCachePath()
	if path == "" {
		return ""
	}

	if c, ok := readUpdateCache(path); ok && time.Since(c.LastCheck) < updateCheckInterval {
		return c.LatestVersion
	}

	latest, err := fetchLatestVersion()
	if err != nil || latest == "" {
		// No cache written, so the next run retries rather than staying quiet for a day because
		// the network happened to be down once.
		return ""
	}

	writeUpdateCache(path, updateCache{LastCheck: time.Now(), LatestVersion: latest})

	return latest
}

// readUpdateCache reports the stored result, and whether it could be read at all. A missing or
// unreadable file is not an error here: it means the check has to run.
func readUpdateCache(path string) (updateCache, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, false
	}

	var c updateCache
	if err := json.Unmarshal(data, &c); err != nil {
		return updateCache{}, false
	}

	return c, true
}

// writeUpdateCache stores the result, ignoring failures: an unwritable cache costs one request
// per command, which is not worth interrupting the user over.
func writeUpdateCache(path string, c updateCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// updateNoticeWait bounds how long the finished command waits for the check. A fresh cache
// answers instantly; anything slower is the network, and the user's command is not held for it.
var updateNoticeWait = 200 * time.Millisecond

// updateCheckResult carries the running check's answer. Nil means no check was started, which is
// what every disabled condition produces.
var updateCheckResult chan string

// startUpdateCheck begins looking for a newer release beside the command the user asked for.
// Called from the root PersistentPreRun, which is the first point where agent mode is known.
func startUpdateCheck(cmdName string) {
	if !updateCheckEnabled(cmdName) {
		return
	}

	// Buffered, so the goroutine never blocks when printUpdateNotice has already given up.
	ch := make(chan string, 1)
	updateCheckResult = ch

	go func() { ch <- runUpdateCheck() }()
}

// printUpdateNotice prints the notice, if there is one, after the command's own output.
func printUpdateNotice() {
	ch := updateCheckResult
	if ch == nil {
		return
	}
	updateCheckResult = nil

	select {
	case latest := <-ch:
		if msg := updateNoticeMessage(latest, Version); msg != "" {
			utils.PrintInfo(msg)
		}
	case <-time.After(updateNoticeWait):
	}
}
