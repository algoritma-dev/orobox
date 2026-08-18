package pipeline

import (
	"os/exec"
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
