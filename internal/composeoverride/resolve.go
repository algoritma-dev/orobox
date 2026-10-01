// Package composeoverride rewrites and inspects the user's Docker Compose override files.
//
// It works on the yaml.v3 node tree instead of decoded structs on purpose: the override is
// the user's file, so `!override` / `!reset` tags, comments and every key Orobox does not know
// about must come out exactly as they went in. The package performs no I/O of its own; the
// only outside knowledge it needs (does a host path exist) is injected by the caller.
package composeoverride

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// Resolve rewrites relative host paths in a compose file to absolute ones against baseDir,
// expanding a leading "~" with home. It edits the yaml.v3 node tree, so tags (!override,
// !reset), comments and unknown keys survive. An empty or comments-only document returns
// (nil, nil).
//
// Compose resolves relative paths against --project-directory, which for Orobox is the
// internal config directory, not the directory holding the user's file. Rewriting them up
// front is what makes `./docker/fixtures` mean what the user thinks it means.
func Resolve(src []byte, baseDir, home string) ([]byte, error) {
	root, err := parse(src)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, nil
	}

	r := resolver{baseDir: baseDir, home: home}
	r.document(root)
	clearMergeTags(root)

	var buf bytes.Buffer
	enc := yamlv3.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encode compose override: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode compose override: %w", err)
	}
	return buf.Bytes(), nil
}

// clearMergeTags drops the explicit "!!merge" tag yaml.v3 puts on a decoded `<<` key. Left in
// place the encoder writes `!!merge <<: *base`, which is valid but not what the user wrote.
// Untagged, the encoder emits the plain `<<` again.
func clearMergeTags(n *yamlv3.Node) {
	if n == nil {
		return
	}
	if n.Kind == yamlv3.AliasNode {
		return // the anchor is visited where it is defined
	}
	if n.Kind == yamlv3.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if isMergeKey(n.Content[i]) {
				n.Content[i].Tag = ""
			}
		}
	}
	for _, c := range n.Content {
		clearMergeTags(c)
	}
}

// parse returns the document node of a compose file, or nil for a document that holds nothing
// (empty, whitespace or comments only). A document whose root is not a mapping is an error:
// returning nil there would make Resolve and Analyze silently report "nothing to do" for a
// file Compose is going to reject.
func parse(src []byte) (*yamlv3.Node, error) {
	var doc yamlv3.Node
	if err := yamlv3.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("parse compose override: %w", err)
	}
	if doc.Kind != yamlv3.DocumentNode || len(doc.Content) == 0 {
		return nil, nil
	}
	if doc.Content[0].Kind != yamlv3.MappingNode {
		return nil, fmt.Errorf("parse compose override: the top level must be a mapping, got %s", kindName(doc.Content[0]))
	}
	return &doc, nil
}

func kindName(n *yamlv3.Node) string {
	switch n.Kind {
	case yamlv3.SequenceNode:
		return "a list"
	case yamlv3.ScalarNode:
		return "a scalar"
	case yamlv3.AliasNode:
		return "an alias"
	}
	return "another kind of node"
}

// maxDepth bounds alias and merge-key chasing so a self-referencing merge cannot loop forever.
const maxDepth = 32

// deref follows an alias to the node it stands for. Compose expands aliases itself, so every
// place that reads a node must see through them; rewriting the anchor target in place then
// fixes every use at once (and is harmless on a second pass, since absolute paths are kept).
func deref(n *yamlv3.Node) *yamlv3.Node {
	for i := 0; n != nil && n.Kind == yamlv3.AliasNode && i < maxDepth; i++ {
		n = n.Alias
	}
	return n
}

// isMergeKey reports whether a mapping key is the YAML `<<` merge key.
func isMergeKey(k *yamlv3.Node) bool {
	return k.Kind == yamlv3.ScalarNode && k.Value == "<<" && (k.Tag == "" || k.Tag == "!!merge")
}

// mergeSources returns the mappings a `<<` entry pulls in, in precedence order: a single
// mapping or alias, or a sequence of them where earlier entries win.
func mergeSources(v *yamlv3.Node) []*yamlv3.Node {
	v = deref(v)
	if v == nil {
		return nil
	}
	switch v.Kind {
	case yamlv3.MappingNode:
		return []*yamlv3.Node{v}
	case yamlv3.SequenceNode:
		var out []*yamlv3.Node
		for _, item := range v.Content {
			if item = deref(item); item != nil && item.Kind == yamlv3.MappingNode {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

// mapGet returns the (alias-resolved) value stored under key in a mapping node, or nil. A key
// the mapping does not define itself is looked up in its `<<` merge sources, as Compose does;
// the mapping's own key always wins, then earlier merge sources.
func mapGet(m *yamlv3.Node, key string) *yamlv3.Node {
	return mapGetDepth(m, key, 0)
}

func mapGetDepth(m *yamlv3.Node, key string, depth int) *yamlv3.Node {
	m = deref(m)
	if m == nil || m.Kind != yamlv3.MappingNode || depth > maxDepth {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && !isMergeKey(m.Content[i]) {
			return deref(m.Content[i+1])
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !isMergeKey(m.Content[i]) {
			continue
		}
		for _, src := range mergeSources(m.Content[i+1]) {
			if v := mapGetDepth(src, key, depth+1); v != nil {
				return v
			}
		}
	}
	return nil
}

// mapEntries calls fn for every key/value pair of a mapping node, merged entries included
// (own keys first, then merge sources in precedence order, each key once). Values are alias
// resolved. It is a no-op on any other kind of node (a `!reset null` value, for instance).
func mapEntries(m *yamlv3.Node, fn func(key string, value *yamlv3.Node)) {
	seen := map[string]bool{}
	mapEntriesDepth(m, seen, fn, 0)
}

func mapEntriesDepth(m *yamlv3.Node, seen map[string]bool, fn func(string, *yamlv3.Node), depth int) {
	m = deref(m)
	if m == nil || m.Kind != yamlv3.MappingNode || depth > maxDepth {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if isMergeKey(m.Content[i]) || seen[m.Content[i].Value] {
			continue
		}
		seen[m.Content[i].Value] = true
		fn(m.Content[i].Value, deref(m.Content[i+1]))
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !isMergeKey(m.Content[i]) {
			continue
		}
		for _, src := range mergeSources(m.Content[i+1]) {
			mapEntriesDepth(src, seen, fn, depth+1)
		}
	}
}

type resolver struct {
	baseDir string
	home    string
}

func (r resolver) document(doc *yamlv3.Node) {
	top := doc.Content[0]
	mapEntries(mapGet(top, "services"), func(_ string, svc *yamlv3.Node) { r.service(svc) })
	for _, section := range []string{"configs", "secrets"} {
		mapEntries(mapGet(top, section), func(_ string, def *yamlv3.Node) {
			r.scalar(mapGet(def, "file"))
		})
	}
	if inc := mapGet(top, "include"); inc != nil && inc.Kind == yamlv3.SequenceNode {
		for _, item := range inc.Content {
			r.include(deref(item))
		}
	}
}

func (r resolver) service(svc *yamlv3.Node) {
	if svc == nil || svc.Kind != yamlv3.MappingNode {
		return
	}
	if vols := mapGet(svc, "volumes"); vols != nil && vols.Kind == yamlv3.SequenceNode {
		for _, v := range vols.Content {
			r.volume(deref(v))
		}
	}
	if b := mapGet(svc, "build"); b != nil {
		switch b.Kind {
		case yamlv3.ScalarNode:
			r.buildContext(b)
		case yamlv3.MappingNode:
			// build.dockerfile is relative to the context, so it must stay as written.
			r.buildContext(mapGet(b, "context"))
		}
	}
	r.scalarOrList(mapGet(svc, "env_file"), func(item *yamlv3.Node) {
		if item.Kind == yamlv3.MappingNode {
			r.scalar(mapGet(item, "path"))
			return
		}
		r.scalar(item)
	})
	r.scalar(mapGet(mapGet(svc, "extends"), "file"))
	r.scalarOrList(mapGet(svc, "label_file"), r.scalar)
}

func (r resolver) include(item *yamlv3.Node) {
	if item == nil {
		return
	}
	switch item.Kind {
	case yamlv3.ScalarNode:
		r.scalar(item)
	case yamlv3.MappingNode:
		r.scalarOrList(mapGet(item, "path"), r.scalar)
		r.scalarOrList(mapGet(item, "env_file"), r.scalar)
		r.scalar(mapGet(item, "project_directory"))
	}
}

// scalarOrList applies fn to a scalar node, or to each item of a sequence node.
func (r resolver) scalarOrList(n *yamlv3.Node, fn func(*yamlv3.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yamlv3.ScalarNode:
		fn(n)
	case yamlv3.SequenceNode:
		for _, item := range n.Content {
			fn(deref(item))
		}
	}
}

// volume rewrites one entry of a service's volumes list, in short or long syntax.
func (r resolver) volume(v *yamlv3.Node) {
	switch v.Kind {
	case yamlv3.ScalarNode:
		r.shortVolume(v)
	case yamlv3.MappingNode:
		// Only bind mounts have a host path; a `volume` source is a volume name even
		// when it happens to look like a path.
		if t := mapGet(v, "type"); t != nil && t.Value == "bind" {
			r.scalar(mapGet(v, "source"))
		}
	}
}

// shortVolume rewrites the SRC of `SRC:DST[:MODE]` when it is a path. A source that does not
// start with "." or "~" is either absolute, interpolated or a named volume, and stays as is.
// The string is only split to isolate SRC; DST and MODE are never touched.
func (r resolver) shortVolume(n *yamlv3.Node) {
	src, rest, found := strings.Cut(n.Value, ":")
	if !found || !isRelative(src) {
		return
	}
	n.Value = r.rewrite(src, false) + ":" + rest
}

// buildContext rewrites a build context, leaving remote contexts (git URLs and the like)
// alone since they are not host paths.
func (r resolver) buildContext(n *yamlv3.Node) {
	if n == nil || n.Kind != yamlv3.ScalarNode {
		return
	}
	if strings.Contains(n.Value, "://") || strings.HasPrefix(n.Value, "git@") {
		return
	}
	r.scalar(n)
}

// scalar rewrites a scalar node that holds a path for a key where Compose resolves a bare
// name such as `foo.env` against the project directory too.
func (r resolver) scalar(n *yamlv3.Node) {
	if n == nil || n.Kind != yamlv3.ScalarNode {
		return
	}
	n.Value = r.rewrite(n.Value, true)
}

// rewrite turns p into an absolute path. With bare set, a name that starts with neither "."
// nor "~" counts as relative as well. Absolute paths, "$"-prefixed values (interpolation is
// the user's responsibility) and the empty string are returned unchanged, as is a "~" when
// home is unknown.
func (r resolver) rewrite(p string, bare bool) string {
	if p == "" || strings.HasPrefix(p, "$") || filepath.IsAbs(p) {
		return p
	}
	if !bare && !isRelative(p) {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if r.home == "" {
			return p
		}
		return filepath.Join(r.home, p[1:])
	}
	// Anything else, including "~user/x", is relative to baseDir, which is what Compose does
	// because it only expands a bare "~".
	return filepath.Join(r.baseDir, p)
}

// isRelative reports whether a short-syntax volume source is a host path. This is the same
// rule Compose uses to tell a path from a named volume.
func isRelative(p string) bool {
	return strings.HasPrefix(p, ".") || strings.HasPrefix(p, "~")
}
