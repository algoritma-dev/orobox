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
// the local file wins. Source lives next to .orobox.yaml; Resolved and ResolvedTest are the
// path-rewritten copies in the internal directory that compose actually reads.
//
// There are two copies because the stack differs by command: db-test exists only when the test
// compose file is included. A tweak to db-test (or to an optional service the project disabled)
// is kept in the copy whose stack defines it and dropped from the other, since compose rejects
// the whole project over a service that has neither an image nor a build context.
var OverrideFiles = []struct{ Source, Resolved, ResolvedTest string }{
	{".orobox.compose.yaml", "compose.project.resolved.yaml", "compose.project.resolved.test.yaml"},
	{".orobox.compose.local.yaml", "compose.local.resolved.yaml", "compose.local.resolved.test.yaml"},
}

// generatedComposeFiles are the files Orobox renders, by mode. The override copy for a mode
// may only extend services these define.
var (
	generatedBaseFiles = []string{"docker-compose.yml", "docker-compose.setup.yml"}
	generatedTestFiles = []string{"docker-compose.yml", "docker-compose.setup.yml", "docker-compose.test.yml"}
)

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
	profiled := map[string]bool{}
	for _, a := range overrideAnalyses {
		merged.MissingPaths = append(merged.MissingPaths, a.MissingPaths...)
		merged.CoreImageOverrides = append(merged.CoreImageOverrides, a.CoreImageOverrides...)
		merged.HasBuild = merged.HasBuild || a.HasBuild
		for _, u := range append(append([]composeoverride.ServiceURL{}, a.URLs...), a.ProfiledURLs...) {
			urls[u.Service] = u.URL // later file overwrites
		}
		// Files are merged in order, so a later file clearing a service's profiles wins over an
		// earlier one setting them, and the other way round.
		for _, name := range a.Profiled {
			profiled[name] = true
		}
		for _, name := range a.Unprofiled {
			profiled[name] = false
		}
	}
	// Profiles declared in one file apply to the service in every file: `up` starts it in none.
	for service, url := range urls {
		if !profiled[service] {
			merged.URLs = append(merged.URLs, composeoverride.ServiceURL{Service: service, URL: url})
		}
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

	baseServices, baseKnown := generatedServices(internalDir, generatedBaseFiles)
	testServices, testKnown := generatedServices(internalDir, generatedTestFiles)
	// Compose applies the files in order, so a later file may tweak a service an earlier one
	// adds (the local file moving a recipe's port) and must not have a build context forced on
	// top of one an earlier file set. Both sets grow as the files are processed.
	builtBefore := map[string]bool{}

	var errs []error
	analyses := make([]composeoverride.Analysis, 0, len(OverrideFiles))
	for i, f := range OverrideFiles {
		drop := func() {
			// A file that cannot be used must not leave a stale copy behind.
			changed = removeIfExists(filepath.Join(internalDir, f.Resolved)) || changed
			changed = removeIfExists(filepath.Join(internalDir, f.ResolvedTest)) || changed
		}
		resolved, ferr := resolveComposeOverride(filepath.Join(projectDir, f.Source), projectDir, home, builtBefore)
		if ferr != nil {
			drop()
			errs = append(errs, fmt.Errorf("%s: %w", f.Source, ferr))
			continue
		}

		base, droppedBase, perr := pruneFor(resolved, baseServices, baseKnown)
		var test []byte
		var droppedTest []string
		if perr == nil {
			test, droppedTest, perr = pruneFor(resolved, testServices, testKnown)
		}
		// URLs, profiles and builds are what `up` runs: the base copy, after pruning. Missing
		// mounts and core image overrides are reported for the test copy as well, so a tweak to
		// db-test is checked too.
		var analysis composeoverride.Analysis
		if perr == nil && base != nil {
			analysis, perr = composeoverride.Analyze(base, CoreServices, pathExists)
		}
		if perr == nil && test != nil {
			var testAnalysis composeoverride.Analysis
			if testAnalysis, perr = composeoverride.Analyze(test, CoreServices, pathExists); perr == nil {
				analysis.MissingPaths = mergeMissing(analysis.MissingPaths, testAnalysis.MissingPaths)
				analysis.CoreImageOverrides = mergeNames(analysis.CoreImageOverrides, testAnalysis.CoreImageOverrides)
			}
		}
		if perr != nil {
			drop()
			errs = append(errs, fmt.Errorf("%s: %w", f.Source, perr))
			continue
		}
		changed = writeOrRemove(filepath.Join(internalDir, f.ResolvedTest), test) || changed
		changed = writeOrRemove(filepath.Join(internalDir, f.Resolved), base) || changed
		// After an earlier file failed, its services are missing from the known set, and this
		// file's tweaks to them would be reported as typos; the real error is already reported.
		if i == 0 || len(errs) == 0 {
			warnDropped(f.Source, droppedBase, droppedTest)
		}
		warnOverride(analysis)
		analyses = append(analyses, analysis)

		baseServices = append(baseServices, serviceNamesOf(base)...)
		testServices = append(testServices, serviceNamesOf(test)...)
		if built, err := composeoverride.BuildServices(resolved); err == nil {
			for name := range built {
				builtBefore[name] = true
			}
		}
	}

	overrideErr = errors.Join(errs...)
	overrideAnalyses = analyses
	return changed, overrideErr
}

// resolveComposeOverride reads and resolves one source file. A missing file, or one holding
// nothing but comments, yields a nil copy and no error: there is simply no override.
func resolveComposeOverride(source, projectDir, home string, builtBefore map[string]bool) ([]byte, error) {
	src, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return composeoverride.ResolveAfter(src, projectDir, home, builtBefore)
}

// serviceNamesOf lists the services of a resolved copy; nil for no copy.
func serviceNamesOf(copy []byte) []string {
	if copy == nil {
		return nil
	}
	names, _ := composeoverride.ServiceNames(copy)
	return names
}

// generatedServices lists the services Orobox's rendered files define for one mode. ok is false
// when the files are not there yet (nothing rendered): there is then nothing to prune against,
// and the override is passed through whole rather than pruned against a guess.
func generatedServices(internalDir string, files []string) (names []string, ok bool) {
	for _, name := range files {
		src, err := os.ReadFile(filepath.Join(internalDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false
		}
		found, err := composeoverride.ServiceNames(src)
		if err != nil {
			return nil, false
		}
		names = append(names, found...)
		ok = true
	}
	return names, ok
}

// pruneFor drops from resolved the services the given stack cannot extend. Without a known
// stack the copy is returned unchanged.
func pruneFor(resolved []byte, known []string, ok bool) ([]byte, []string, error) {
	if resolved == nil || !ok {
		return resolved, nil, nil
	}
	return composeoverride.Prune(resolved, known)
}

// warnDropped reports the services dropped from both copies: they extend nothing the stack ever
// defines (a disabled optional service, a typo). A service dropped only from the base copy,
// such as db-test, exists for test commands and is silently kept for them.
func warnDropped(source string, droppedBase, droppedTest []string) {
	inTest := map[string]bool{}
	for _, svc := range droppedTest {
		inTest[svc] = true
	}
	for _, svc := range droppedBase {
		if inTest[svc] {
			warnOnce(fmt.Sprintf("%s in %s extends a service that is not part of this stack (disabled, or misspelled); it is ignored", svc, source))
		}
	}
}

// writeOrRemove writes content to dest when it differs, or removes dest when content is nil,
// and reports whether anything changed on disk.
func writeOrRemove(dest string, content []byte) bool {
	if content == nil {
		return removeIfExists(dest)
	}
	if old, err := os.ReadFile(dest); err == nil && bytes.Equal(old, content) {
		return false
	}
	if err := os.WriteFile(dest, content, 0644); err != nil {
		// The runner would then read a stale copy; removing it at least fails loudly.
		removeIfExists(dest)
		return true
	}
	return true
}

func removeIfExists(path string) bool {
	return os.Remove(path) == nil
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
		consequence := "Docker creates an empty directory there"
		if p.LongSyntax {
			consequence = "Docker refuses to start the container"
		}
		warnOnce(fmt.Sprintf("bind mount source does not exist: %s — %s", p.Path, consequence))
	}
	for _, svc := range a.CoreImageOverrides {
		warnOnce(fmt.Sprintf("%s redefines image; it is detached from oro_version and the custom layer", svc))
	}
}

// appendOverrideArgs adds a -f for each resolved override that exists, last, so the overrides
// win over everything Orobox generates (the test file included). test picks the copies pruned
// against the stack that includes the test compose file.
func appendOverrideArgs(args []string, internalDir string, test bool) []string {
	for _, f := range OverrideFiles {
		name := f.Resolved
		if test {
			name = f.ResolvedTest
		}
		resolved := filepath.Join(internalDir, name)
		if _, err := os.Stat(resolved); err == nil {
			args = append(args, "-f", resolved)
		}
	}
	return args
}

// mergeMissing appends to a the paths of b it does not list yet.
func mergeMissing(a, b []composeoverride.MissingPath) []composeoverride.MissingPath {
	seen := map[string]bool{}
	for _, p := range a {
		seen[p.Path] = true
	}
	for _, p := range b {
		if !seen[p.Path] {
			seen[p.Path] = true
			a = append(a, p)
		}
	}
	return a
}

// mergeNames appends to a the names of b it does not list yet.
func mergeNames(a, b []string) []string {
	seen := map[string]bool{}
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if !seen[n] {
			seen[n] = true
			a = append(a, n)
		}
	}
	return a
}
