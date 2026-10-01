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
	MissingPaths       []string     // absolute bind sources that do not exist on the host
	CoreImageOverrides []string     // core services whose `image` is redefined
	HasBuild           bool         // any service has a `build` key
	URLs               []ServiceURL // services with a dev.orobox.url label, sorted by service
}

// Analyze inspects a resolved compose file (relative paths already made absolute by Resolve).
// exists is injected so the package stays free of I/O. An empty document yields a zero Analysis.
//
// Only bind sources are checked: a missing host path makes Docker silently create an empty
// root-owned directory, which is almost never what the user meant. Named volumes have no host
// path and are never reported.
func Analyze(resolved []byte, coreServices []string, exists func(string) bool) (Analysis, error) {
	var a Analysis
	doc, err := parse(resolved)
	if err != nil {
		return a, err
	}
	if doc == nil {
		return a, nil
	}

	seen := map[string]bool{}
	checkPath := func(p string) {
		if p == "" || !filepath.IsAbs(p) || seen[p] {
			return
		}
		seen[p] = true
		if !exists(p) {
			a.MissingPaths = append(a.MissingPaths, p)
		}
	}

	mapEntries(mapGet(doc.Content[0], "services"), func(name string, svc *yamlv3.Node) {
		if svc == nil || svc.Kind != yamlv3.MappingNode {
			return
		}
		if mapGet(svc, "image") != nil && slices.Contains(coreServices, name) {
			a.CoreImageOverrides = append(a.CoreImageOverrides, name)
		}
		if mapGet(svc, "build") != nil {
			a.HasBuild = true
		}
		if vols := mapGet(svc, "volumes"); vols != nil && vols.Kind == yamlv3.SequenceNode {
			for _, v := range vols.Content {
				checkPath(bindSource(deref(v)))
			}
		}
		if url, ok := labelValue(mapGet(svc, "labels"), urlLabel); ok {
			a.URLs = append(a.URLs, ServiceURL{Service: name, URL: url})
		}
	})

	sort.SliceStable(a.URLs, func(i, j int) bool { return a.URLs[i].Service < a.URLs[j].Service })
	return a, nil
}

// bindSource returns the host path of a volume entry, or "" when the entry is not a bind mount.
func bindSource(v *yamlv3.Node) string {
	switch v.Kind {
	case yamlv3.ScalarNode:
		src, _, found := strings.Cut(v.Value, ":")
		if !found {
			return "" // a lone path is an anonymous volume at that container path
		}
		return src
	case yamlv3.MappingNode:
		if t := mapGet(v, "type"); t == nil || t.Value != "bind" {
			return ""
		}
		if s := mapGet(v, "source"); s != nil && s.Kind == yamlv3.ScalarNode {
			return s.Value
		}
	}
	return ""
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
