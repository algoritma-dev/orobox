package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// fallbackExcludes is what a host directory is filtered by when git cannot answer: outside a
// repository, or on a runner image without git. It is deliberately conservative — these are the
// directories that are always generated, never committed.
var fallbackExcludes = []string{
	"vendor",
	"node_modules",
	"var",
	".git",
	"vendor-bin/qa/vendor",
	"vendor-bin/qa/node_modules",
	"vendor-bin/deploy/vendor",
}

// HostExcludes lists what must not be uploaded from a host directory.
//
// It is a correctness requirement before it is an optimization. Every step overlays the sources on
// top of a layer that already holds the vendor tree, so a developer's local vendor/ — carrying dev
// dependencies — would end up inside the production artifact. Uploading var/cache and node_modules
// on every run would also make each invocation pay for gigabytes the pipeline builds itself.
//
// The list comes from git rather than from a hardcoded set because the repository already encodes
// the distinction the pipeline needs and a fixed list cannot: public/build must be excluded for a
// project that builds its assets in the pipeline and kept for one that commits them
// (pre_built_assets_enabled), and that is exactly what its .gitignore says.
//
// .git is appended unconditionally: git never reports its own directory as ignored, and uploading
// it is pure cost — no step reads history.
func HostExcludes(projectDir string) []string {
	cmd := exec.Command("git", "ls-files", "-oi", "--directory", "--exclude-standard")
	cmd.Dir = projectDir

	output, err := cmd.Output()
	if err != nil {
		return slices.Clone(fallbackExcludes)
	}

	var excludes []string
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			excludes = append(excludes, line)
		}
	}

	if !slices.Contains(excludes, ".git") {
		excludes = append(excludes, ".git")
	}
	return excludes
}

// MissingExcludes returns the exclude patterns that name nothing on disk under projectDir.
//
// It exists for diagnosis, not validation. An exclude for a path that is not there excludes
// nothing and is harmless, and the list is legitimately a snapshot: git reports what was in the
// tree when it ran, and orobox runs containers that write into a project checkout — for a project
// install the checkout *is* the container's application root — between that call and the tree
// being uploaded.
//
// The reason to surface it is a CI failure that reported
// "failed to make directory content hashed: stat vendor-bin/qa: no such file or directory"
// against a tree whose exclude list was only ever logged as a count. Naming the absent patterns
// makes the next occurrence say which one it was, or rule the exclude list out entirely.
//
// Patterns are matched literally. A glob that happens to match nothing is reported too, which is
// the conservative direction for a diagnostic.
func MissingExcludes(projectDir string, excludes []string) []string {
	var missing []string
	for _, pattern := range excludes {
		clean := strings.TrimSuffix(strings.TrimSpace(pattern), "/")
		if clean == "" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(projectDir, clean)); err != nil {
			missing = append(missing, pattern)
		}
	}
	return missing
}
