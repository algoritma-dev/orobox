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
