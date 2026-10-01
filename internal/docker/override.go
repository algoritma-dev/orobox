package docker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/algoritma-dev/orobox/internal/composeoverride"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// OverrideFiles are the user's compose override files, in the order they are appended to the
// compose command: the committed team file first, the git-ignored personal one after it, so
// the local file wins. Source lives next to .orobox.yaml; Resolved is the path-rewritten copy
// in the internal directory that compose actually reads.
var OverrideFiles = []struct{ Source, Resolved string }{
	{".orobox.compose.yaml", "compose.project.resolved.yaml"},
	{".orobox.compose.local.yaml", "compose.local.resolved.yaml"},
}

// CoreServices are the Oro services Orobox generates. An override that redefines `image` on
// one of them detaches it from oro_version and from the project's custom layer.
var CoreServices = []string{"application", "web", "php-fpm-app", "ws", "consumer", "cron", "volume-init", "web-init"}

// What the last writeComposeOverrides found. Package state because the compose runners and
// `orobox up` run long after EnsureDockerCompose and need the verdict without re-reading the
// files; every writeComposeOverrides call replaces it wholesale.
var (
	overrideErr      error
	overrideAnalyses []composeoverride.Analysis
)

// OverrideError returns the error of the last writeComposeOverrides call, nil when both files
// were fine. Every runner returns it before running compose: with a broken override there is
// no correct stack to run, and silently dropping the file would start the wrong one.
func OverrideError() error {
	return overrideErr
}

// OverrideAnalysis merges the analysis of both override files. The local file wins over the
// team file, as it does in compose, so a service advertised by both keeps the local URL.
func OverrideAnalysis() composeoverride.Analysis {
	var merged composeoverride.Analysis
	urls := map[string]string{}
	for _, a := range overrideAnalyses {
		merged.MissingPaths = append(merged.MissingPaths, a.MissingPaths...)
		merged.CoreImageOverrides = append(merged.CoreImageOverrides, a.CoreImageOverrides...)
		merged.HasBuild = merged.HasBuild || a.HasBuild
		for _, u := range a.URLs {
			urls[u.Service] = u.URL // later file overwrites
		}
	}
	for service, url := range urls {
		merged.URLs = append(merged.URLs, composeoverride.ServiceURL{Service: service, URL: url})
	}
	sort.Slice(merged.URLs, func(i, j int) bool { return merged.URLs[i].Service < merged.URLs[j].Service })
	return merged
}

// writeComposeOverrides refreshes the resolved copies of the override files in internalDir and
// reports whether any of them was written or removed.
//
// A file that is missing, or holds nothing but comments, leaves no resolved copy: a leftover
// from an earlier run would keep applying an override the user has since deleted. A file that
// cannot be used removes its copy for the same reason (a stale copy must never reach compose)
// and is recorded in the returned error, which names the source file. The other file is still
// processed so one broken file does not hide the state of the other.
func writeComposeOverrides(internalDir, projectDir string) (changed bool, err error) {
	// Without a home directory Resolve leaves `~` alone; compose then reports the path.
	home, herr := os.UserHomeDir()
	if herr != nil {
		home = ""
	}

	var errs []error
	analyses := make([]composeoverride.Analysis, 0, len(OverrideFiles))
	for _, f := range OverrideFiles {
		fileChanged, analysis, ferr := syncComposeOverride(
			filepath.Join(projectDir, f.Source), filepath.Join(internalDir, f.Resolved), projectDir, home)
		changed = changed || fileChanged
		if ferr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Source, ferr))
			continue
		}
		warnOverride(analysis)
		analyses = append(analyses, analysis)
	}

	overrideErr = errors.Join(errs...)
	overrideAnalyses = analyses
	return changed, overrideErr
}

// syncComposeOverride resolves one source file into dest. On every path that does not leave a
// valid resolved copy behind, dest is removed.
func syncComposeOverride(source, dest, projectDir, home string) (changed bool, analysis composeoverride.Analysis, err error) {
	drop := func() bool { return os.Remove(dest) == nil }

	src, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return drop(), analysis, nil
	}
	if err != nil {
		return drop(), analysis, err
	}

	resolved, err := composeoverride.Resolve(src, projectDir, home)
	if err != nil {
		return drop(), analysis, err
	}
	if resolved == nil {
		return drop(), analysis, nil
	}

	analysis, err = composeoverride.Analyze(resolved, CoreServices, pathExists)
	if err != nil {
		return drop(), composeoverride.Analysis{}, err
	}

	if old, rerr := os.ReadFile(dest); rerr == nil && bytes.Equal(old, resolved) {
		return false, analysis, nil
	}
	if err := os.WriteFile(dest, resolved, 0644); err != nil {
		return drop(), composeoverride.Analysis{}, err
	}
	return true, analysis, nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// warned remembers the warnings already printed by this process. EnsureDockerCompose runs
// several times in one command (init alone calls it three times) and re-analyzes the same
// override files each time, so without this the same line would repeat.
var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

// warnOnce prints a warning the first time its exact text is seen in this process, and stays
// silent afterwards. Distinct texts (another missing path, another service) still print.
func warnOnce(message string) {
	warnedMu.Lock()
	seen := warned[message]
	warned[message] = true
	warnedMu.Unlock()
	if !seen {
		utils.PrintWarning(message)
	}
}

// resetWarned forgets what warnOnce printed. Tests call it so one test's warning cannot hide
// the same text in the next.
func resetWarned() {
	warnedMu.Lock()
	warned = map[string]bool{}
	warnedMu.Unlock()
}

// warnOverride prints one warning per finding. These are warnings, not errors: both situations
// are sometimes intended, but when they are not the failure shows up far from its cause.
func warnOverride(a composeoverride.Analysis) {
	for _, p := range a.MissingPaths {
		warnOnce(fmt.Sprintf("bind mount source does not exist: %s — Docker would create an empty directory", p))
	}
	for _, svc := range a.CoreImageOverrides {
		warnOnce(fmt.Sprintf("%s redefines image; it is detached from oro_version and the custom layer", svc))
	}
}

// appendOverrideArgs adds a -f for each resolved override that exists, last, so the overrides
// win over everything Orobox generates (the test file included).
func appendOverrideArgs(args []string, internalDir string) []string {
	for _, f := range OverrideFiles {
		resolved := filepath.Join(internalDir, f.Resolved)
		if _, err := os.Stat(resolved); err == nil {
			args = append(args, "-f", resolved)
		}
	}
	return args
}
