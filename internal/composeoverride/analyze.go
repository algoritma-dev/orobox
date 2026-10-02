package composeoverride

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// urlLabel is the service label Orobox reads to learn which URL a user-added service serves.
const urlLabel = "dev.orobox.url"

// ServiceURL is a service that advertises a browsable URL through the dev.orobox.url label.
type ServiceURL struct{ Service, URL string }

// Analysis is what Orobox needs to know about a resolved override before running compose.
type Analysis struct {
	MissingPaths       []MissingPath // absolute bind sources that do not exist on the host
	CoreImageOverrides []string      // core services whose `image` is redefined
	HasBuild           bool          // any service has a `build` key
	URLs               []ServiceURL  // services with a dev.orobox.url label, sorted by service
	// Profiled are the services that declare profiles, sorted. `up` starts none of them; a
	// caller merging several files filters the URLs of every file against all of them.
	Profiled []string
	// Unprofiled are the services whose profiles this file clears (`profiles: !reset []`), sorted:
	// a later file undoing the profiles an earlier one gave.
	Unprofiled []string
	// ProfiledURLs are the dev.orobox.url labels of services this file profiles: left out of URLs,
	// but a later file clearing the profiles makes them apply again.
	ProfiledURLs []ServiceURL
}

// MissingPath is a bind source that does not exist. The syntax matters because Docker treats
// the two differently: a short-syntax source is created as an empty root-owned directory, a
// long-syntax one makes the container fail to start.
type MissingPath struct {
	Path       string
	LongSyntax bool
}

// Analyze inspects a resolved compose file (relative paths already made absolute by Resolve).
// exists is injected so the package stays free of I/O. Every document of the file is inspected
// and the findings are aggregated; an empty file yields a zero Analysis.
//
// Only bind sources are checked: either way a missing host path is almost never what the user
// meant. Named volumes have no host path and are never reported, nor are sources that compose
// still has to interpolate, nor long-syntax binds that ask compose to create the path.
func Analyze(resolved []byte, coreServices []string, exists func(string) bool) (Analysis, error) {
	var a Analysis
	docs, err := parseAll(resolved)
	if err != nil {
		return a, err
	}

	seen := map[string]bool{}
	checkPath := func(p string, long bool) {
		p, literal := literalPath(p)
		if !literal || p == "" || !filepath.IsAbs(p) || seen[p] {
			return
		}
		seen[p] = true
		if !exists(p) {
			a.MissingPaths = append(a.MissingPaths, MissingPath{Path: p, LongSyntax: long})
		}
	}

	// Compose merges the documents in order, so a URL set by a later document wins and a
	// service's profiles are those of the last document that mentions them.
	coreSeen := map[string]bool{}
	urls := map[string]string{}
	profiled := map[string]bool{}
	for _, doc := range docs {
		mapEntries(mapGet(doc.Content[0], "services"), func(name string, svc *yamlv3.Node) {
			if svc == nil || svc.Kind != yamlv3.MappingNode {
				return
			}
			if isSet(mapGet(svc, "image")) && slices.Contains(coreServices, name) && !coreSeen[name] {
				coreSeen[name] = true
				a.CoreImageOverrides = append(a.CoreImageOverrides, name)
			}
			if isSet(mapGet(svc, "build")) {
				a.HasBuild = true
			}
			if vols := mapGet(svc, "volumes"); vols != nil && vols.Kind == yamlv3.SequenceNode {
				for _, v := range vols.Content {
					if src, long, ok := bindSource(deref(v)); ok {
						checkPath(src, long)
					}
				}
			}
			// The last document that mentions profiles decides: a later `profiles: !reset []`
			// (or an empty list) clears what an earlier one set.
			if p := mapGet(svc, "profiles"); p != nil {
				profiled[name] = isSet(p) && hasProfiles(p)
			}
			if url, ok := labelValue(mapGet(svc, "labels"), urlLabel); ok {
				urls[name] = url
			}
		})
	}

	// `up` starts no service that declares profiles, so advertising its URL would point at
	// nothing.
	for name, url := range urls {
		if !profiled[name] {
			a.URLs = append(a.URLs, ServiceURL{Service: name, URL: url})
		} else {
			a.ProfiledURLs = append(a.ProfiledURLs, ServiceURL{Service: name, URL: url})
		}
	}
	sort.Slice(a.ProfiledURLs, func(i, j int) bool { return a.ProfiledURLs[i].Service < a.ProfiledURLs[j].Service })
	for name, on := range profiled {
		if on {
			a.Profiled = append(a.Profiled, name)
		} else {
			a.Unprofiled = append(a.Unprofiled, name)
		}
	}
	sort.Strings(a.Profiled)
	sort.Strings(a.Unprofiled)
	sort.Slice(a.URLs, func(i, j int) bool { return a.URLs[i].Service < a.URLs[j].Service })
	return a, nil
}

// hasProfiles reports whether a profiles value names at least one profile.
func hasProfiles(n *yamlv3.Node) bool {
	if !isSet(n) {
		return false
	}
	switch n.Kind {
	case yamlv3.SequenceNode:
		return len(n.Content) > 0
	case yamlv3.ScalarNode:
		return n.Value != ""
	}
	return false
}

// literalPath returns the path a resolved value stands for once compose has interpolated it:
// "$$" is a literal "$". A value with any other "$" depends on the environment compose runs in,
// so it cannot be checked here and literal is false.
func literalPath(p string) (path string, literal bool) {
	if strings.Contains(strings.ReplaceAll(p, "$$", ""), "$") {
		return "", false
	}
	return strings.ReplaceAll(p, "$$", "$"), true
}

// bindSource returns the host path of a volume entry and whether it is in long syntax. ok is
// false when the entry is not a bind mount, or is a long-syntax bind compose creates when
// missing (bind.create_host_path).
func bindSource(v *yamlv3.Node) (src string, long, ok bool) {
	switch v.Kind {
	case yamlv3.ScalarNode:
		src, _, found := strings.Cut(v.Value, ":")
		if !found {
			return "", false, false // a lone path is an anonymous volume at that container path
		}
		return src, false, true
	case yamlv3.MappingNode:
		if t := mapGet(v, "type"); t == nil || t.Value != "bind" {
			return "", true, false
		}
		if c := mapGet(mapGet(v, "bind"), "create_host_path"); c != nil && c.Kind == yamlv3.ScalarNode && c.Value == "true" {
			return "", true, false
		}
		if s := mapGet(v, "source"); s != nil && s.Kind == yamlv3.ScalarNode {
			return s.Value, true, true
		}
	}
	return "", false, false
}

// labelValue looks key up in a labels node, which Compose allows as a mapping or as a list of
// `key=value` strings.
func labelValue(labels *yamlv3.Node, key string) (string, bool) {
	if labels == nil {
		return "", false
	}
	switch labels.Kind {
	case yamlv3.MappingNode:
		if v := mapGet(labels, key); v != nil && v.Kind == yamlv3.ScalarNode {
			return v.Value, true
		}
	case yamlv3.SequenceNode:
		for _, item := range labels.Content {
			item = deref(item)
			if item == nil {
				continue
			}
			if k, v, found := strings.Cut(item.Value, "="); found && k == key {
				return v, true
			}
		}
	}
	return "", false
}
