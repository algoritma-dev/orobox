package utils

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// GetLatestTag returns the latest stable tag for a given version prefix.
// If the version is already a specific tag, it returns it.
func GetLatestTag(repoURL, versionPrefix string) (string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list tags: %w", err)
	}

	var tags []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		tagRef := parts[1]
		if !strings.HasPrefix(tagRef, "refs/tags/") {
			continue
		}
		tagName := strings.TrimPrefix(tagRef, "refs/tags/")
		if strings.HasSuffix(tagName, "^{}") {
			continue // Skip dereferenced tags
		}

		// Skip pre-releases (beta, rc) unless the versionPrefix already includes it
		if strings.Contains(tagName, "-") && !strings.Contains(versionPrefix, "-") {
			continue
		}

		// If version prefix is X.Y, we only want tags starting with X.Y.
		if tagName == versionPrefix || strings.HasPrefix(tagName, versionPrefix+".") {
			tags = append(tags, tagName)
		}
	}

	if len(tags) == 0 {
		return versionPrefix, nil // Fallback to version prefix if no tags found
	}

	// Sort tags using a simple version comparison
	sort.Slice(tags, func(i, j int) bool {
		return compareVersions(tags[i], tags[j]) < 0
	})

	// Return the latest one
	return tags[len(tags)-1], nil
}

func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		p1, p2 := parts1[i], parts2[i]

		// Handle cases like "0-rc"
		n1, err1 := strconv.Atoi(p1)
		n2, err2 := strconv.Atoi(p2)

		if err1 == nil && err2 == nil {
			if n1 != n2 {
				return n1 - n2
			}
			continue
		}

		// If not both are numbers, use string comparison
		if p1 != p2 {
			return strings.Compare(p1, p2)
		}
	}

	return len(parts1) - len(parts2)
}

// StagedFiles returns the files a commit staged in dir, as paths relative to dir.
//
// --relative is what makes the result usable without a second path mapping: git reports staged
// paths from the repository root, while every caller here works from the directory holding
// .orobox.yaml, which on a bundle checkout is not the same directory. It also drops the staged
// files that live outside dir entirely, which is what the QA tools would have had to ignore
// anyway.
//
// --diff-filter=ACMR leaves out the deletions: a file this commit removes is not a file any tool
// can open.
//
// The staged set is exactly the subset an IDE selected. PhpStorm stages the checked files — and,
// for a partial commit, the checked hunks — before it runs the hook, so the index is already the
// commit's own contents by the time this reads it.
func StagedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "--relative", "-z")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not list the staged files: %w", err)
	}

	var files []string
	for _, name := range strings.Split(string(output), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// ShellQuote renders s as a single POSIX sh word, so a path can be dropped into a shell line
// whatever it contains. The shell lines Orobox builds are assembled by hand rather than by an
// argv, which is why the quoting has to be too.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
