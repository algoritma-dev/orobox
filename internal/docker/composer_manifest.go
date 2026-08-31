package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// manifestRepository is the subset of a Composer repository definition this package reads.
type manifestRepository struct {
	URL string `json:"url"`
}

// manifestRepoURLs returns the repository URLs declared in the composer.json at dir.
// Composer accepts `repositories` as either a list of definitions or a map of name to
// definition, so both shapes are tried.
//
// A missing, unreadable or malformed file yields no URLs rather than an error: the project
// may not be scaffolded yet, and the caller's decision then rests on the repositories
// declared in .orobox.yaml alone. Composer itself reports a broken composer.json far better
// than this parser could.
func manifestRepoURLs(dir string) []string {
	if dir == "" {
		return nil
	}

	content, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return nil
	}

	var manifest struct {
		Repositories json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil || len(manifest.Repositories) == 0 {
		return nil
	}

	var entries []manifestRepository
	if err := json.Unmarshal(manifest.Repositories, &entries); err != nil {
		named := map[string]manifestRepository{}
		if err := json.Unmarshal(manifest.Repositories, &named); err != nil {
			return nil
		}
		for _, entry := range named {
			entries = append(entries, entry)
		}
	}

	var urls []string
	for _, entry := range entries {
		if entry.URL != "" {
			urls = append(urls, entry.URL)
		}
	}
	return urls
}
