package qatools

import "strings"

// BaselineFile is the name PHPStan gives a generated baseline, and the name Orobox keeps. The file
// is written next to the project's own phpstan.neon at the source root, so the `includes` entry
// that activates it is a bare filename: NEON resolves a relative include against the file holding
// it, which is the project config in both install types and under the merged config as well.
const BaselineFile = "phpstan-baseline.neon"

// BaselinePath is the file `orobox qa --generate-baseline` writes, for a source root. The argument
// is a container path when the caller is building a tool invocation and a host path when it is
// about to read or write the file itself; the two are the same directory through the bind mount.
func BaselinePath(sourceRoot string) string {
	return sourceRoot + "/" + BaselineFile
}

// EnsureBaselineInclude returns the project's phpstan.neon with the generated baseline in its
// `includes` list, and reports whether anything had to be added.
//
// The include is wired up rather than left to the reader because a baseline nothing includes is a
// file with no effect: the generation run would look like it worked and the next analysis would
// report every error again.
//
// An existing `includes:` block is extended instead of a second one being written — NEON takes the
// last value for a repeated key, so a second block would silently drop the first one's entries,
// which for this file is the whole shared standard.
func EnsureBaselineInclude(neon string) (string, bool) {
	if hasBaselineInclude(neon) {
		return neon, false
	}

	entry := "    - " + BaselineFile
	lines := strings.Split(neon, "\n")
	for i, line := range lines {
		// Only a top-level key counts: an indented `includes:` belongs to some other section and
		// adding to it would move the baseline under a key that does not read includes at all.
		if strings.TrimRight(line, " \t") != "includes:" {
			continue
		}
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:i+1]...)
		out = append(out, entry)
		out = append(out, lines[i+1:]...)
		return strings.Join(out, "\n"), true
	}

	block := "includes:\n" + entry + "\n"
	if strings.TrimSpace(neon) == "" {
		return block, true
	}
	return block + "\n" + neon, true
}

// hasBaselineInclude reports whether the config already names the baseline file.
//
// Comments are stripped first: the stub Orobox scaffolds explains the baseline in prose, and
// matching that sentence would leave the include unwritten on the one file that most needs it.
func hasBaselineInclude(neon string) bool {
	for _, line := range strings.Split(neon, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.Contains(line, BaselineFile) {
			return true
		}
	}
	return false
}
