package pipeline

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// lockLayerAlwaysPaths are mounted into the dependency layer whatever the manifest says.
//
// The two composer files are the layer's whole cache key. vendor-bin/qa is there because
// qatools.ComposerInstallCommand branches on whether that directory holds a committed manifest:
// mounting it only with the sources would make the layer install the tools with ':*' and then
// throw away the versions the project pinned on purpose. patches/ covers projects using
// cweagans/composer-patches, whose install fails without it.
var lockLayerAlwaysPaths = []string{"composer.json", "composer.lock", "patches", "vendor-bin/qa"}

// LockLayerPaths lists the repository-relative paths `composer install` needs before the
// application sources exist. The result is sorted and deduplicated so the layer's cache key does
// not depend on the order composer.json happens to list its repositories in.
//
// Every path is a candidate: the caller mounts the ones that exist in the clone. A manifest that
// does not parse yields the fixed list, because composer reports a broken composer.json far
// better than this parser could.
func LockLayerPaths(composerJSON []byte) []string {
	paths := map[string]bool{}
	for _, always := range lockLayerAlwaysPaths {
		paths[always] = true
	}
	for _, dir := range pathRepositories(composerJSON) {
		paths[dir] = true
	}

	result := make([]string, 0, len(paths))
	for p := range paths {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// repository is the part of a composer.json repositories entry this parser cares about.
type repository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// pathRepositories returns the in-repository directories referenced by `path` repositories.
// Composer accepts the repositories key as either a list or a named object, so both are tried.
func pathRepositories(composerJSON []byte) []string {
	var manifest struct {
		Repositories json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal(composerJSON, &manifest); err != nil || len(manifest.Repositories) == 0 {
		return nil
	}

	var entries []repository
	if err := json.Unmarshal(manifest.Repositories, &entries); err != nil {
		named := map[string]repository{}
		if err := json.Unmarshal(manifest.Repositories, &named); err != nil {
			return nil
		}
		for _, entry := range named {
			entries = append(entries, entry)
		}
	}

	var dirs []string
	for _, entry := range entries {
		if entry.Type != "path" {
			continue
		}
		if dir := repositoryDir(entry.URL); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// repositoryDir turns a path repository URL into a directory inside the clone, or an empty
// string when it points outside it. A glob is truncated at its first wildcard segment, because
// the layer mounts the directory that contains the packages rather than each package.
func repositoryDir(url string) string {
	if url == "" || strings.HasPrefix(url, "/") {
		return ""
	}

	var segments []string
	for _, segment := range strings.Split(path.Clean(url), "/") {
		if strings.ContainsAny(segment, "*?[") {
			break
		}
		segments = append(segments, segment)
	}

	dir := path.Join(segments...)
	if dir == "" || dir == "." || dir == ".." || strings.HasPrefix(dir, "../") {
		return ""
	}
	return dir
}
