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
// throw away the versions the project pinned on purpose. patches/ is the conventional directory
// of projects using cweagans/composer-patches, whose install fails without it; a project keeping
// its patches elsewhere is covered by patchPaths reading the manifest.
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
	for _, patch := range patchPaths(composerJSON) {
		paths[patch] = true
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

// PatchesFile returns the repository-relative path named by extra.patches-file, or an empty
// string when the manifest does not use one. The file lists patches the same way extra.patches
// does, so the caller reads it and feeds the contents to PatchFilePaths.
func PatchesFile(composerJSON []byte) string {
	var manifest struct {
		Extra struct {
			PatchesFile string `json:"patches-file"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(composerJSON, &manifest); err != nil {
		return ""
	}
	return relativePath(manifest.Extra.PatchesFile)
}

// PatchFilePaths returns the paths the patches declared in a patches-file need. The file holds
// the same "patches" mapping composer.json carries under extra.
func PatchFilePaths(patchesFileJSON []byte) []string {
	var file struct {
		Patches json.RawMessage `json:"patches"`
	}
	if err := json.Unmarshal(patchesFileJSON, &file); err != nil {
		return nil
	}
	return patchPathsFrom(file.Patches)
}

// patchPaths returns the in-repository paths cweagans/composer-patches reads while installing:
// the directory holding each local patch declared under extra.patches, plus the patches-file
// itself. Without them the plugin finds no file at the declared path and tries to download it as
// a URL instead, which fails inside RemoteFilesystem::copy() with a null origin. A patch declared
// as an http(s) URL needs nothing mounted.
func patchPaths(composerJSON []byte) []string {
	var manifest struct {
		Extra struct {
			Patches     json.RawMessage `json:"patches"`
			PatchesFile string          `json:"patches-file"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(composerJSON, &manifest); err != nil {
		return nil
	}

	paths := patchPathsFrom(manifest.Extra.Patches)
	// The patches-file itself is mounted the same way a patch is, so a nested one arrives with
	// the directory it sits in rather than as a path the layer would take for a directory.
	if file := patchMount(manifest.Extra.PatchesFile); file != "" {
		paths = append(paths, file)
	}
	return paths
}

// patchPathsFrom turns a "patches" mapping into mountable paths. Composer-patches accepts a
// package's patches either as a description/path object or as a list, whose entries are a path or
// an object carrying it, so all three shapes are tried.
func patchPathsFrom(patches json.RawMessage) []string {
	if len(patches) == 0 {
		return nil
	}
	var byPackage map[string]json.RawMessage
	if err := json.Unmarshal(patches, &byPackage); err != nil {
		return nil
	}

	var paths []string
	for _, entries := range byPackage {
		for _, url := range patchURLs(entries) {
			if p := patchMount(url); p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// patchURLs extracts the patch locations one package declares, whichever shape it used.
func patchURLs(entries json.RawMessage) []string {
	var described map[string]string
	if err := json.Unmarshal(entries, &described); err == nil {
		urls := make([]string, 0, len(described))
		for _, url := range described {
			urls = append(urls, url)
		}
		return urls
	}

	var list []json.RawMessage
	if err := json.Unmarshal(entries, &list); err != nil {
		return nil
	}
	var urls []string
	for _, entry := range list {
		var url string
		if err := json.Unmarshal(entry, &url); err == nil {
			urls = append(urls, url)
			continue
		}
		var object struct {
			URL  string `json:"url"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(entry, &object); err != nil {
			continue
		}
		if object.URL != "" {
			urls = append(urls, object.URL)
		}
		if object.Path != "" {
			urls = append(urls, object.Path)
		}
	}
	return urls
}

// patchMount turns a patch location into the path to mount: the directory that holds it, so a
// package's patches travel together, or the file itself when it sits at the repository root.
func patchMount(url string) string {
	clean := relativePath(url)
	if clean == "" {
		return ""
	}
	if dir := path.Dir(clean); dir != "." {
		return dir
	}
	return clean
}

// relativePath cleans a manifest path and rejects anything the pipeline cannot mount: a URL, an
// absolute path, or a path reaching outside the clone.
func relativePath(url string) string {
	if url == "" || strings.Contains(url, "://") || strings.HasPrefix(url, "/") {
		return ""
	}
	clean := path.Clean(url)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}
