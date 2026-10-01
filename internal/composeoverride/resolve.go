// Package composeoverride rewrites and inspects the user's Docker Compose override files.
//
// It works on the yaml.v3 node tree instead of decoded structs on purpose: the override is
// the user's file, so `!override` / `!reset` tags, comments and every key Orobox does not know
// about must come out exactly as they went in. The package performs no I/O of its own; the
// only outside knowledge it needs (does a host path exist) is injected by the caller.
package composeoverride

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// Resolve rewrites relative host paths in a compose file to absolute ones against baseDir,
// expanding a leading "~" with home. It edits the yaml.v3 node tree, so tags (!override,
// !reset), comments and unknown keys survive. Every document of a multi-document file is
// resolved; empty or comments-only documents are dropped, and a file with nothing else returns
// (nil, nil).
//
// Compose resolves relative paths against --project-directory, which for Orobox is the
// internal config directory, not the directory holding the user's file. Rewriting them up
// front is what makes `./docker/fixtures` mean what the user thinks it means.
func Resolve(src []byte, baseDir, home string) ([]byte, error) {
	docs, err := parseAll(src)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	r := &resolver{baseDir: baseDir, home: home}
	for _, doc := range docs {
		r.document(doc)
		if r.err != nil {
			return nil, r.err
		}
	}
	return encodeAll(docs)
}

// encodeAll writes docs back as one file, separated by `---` as the encoder does for
// successive documents.
func encodeAll(docs []*yamlv3.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yamlv3.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		clearMergeTags(doc)
		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode compose override: %w", err)
		}
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

// parseAll returns the document nodes of a compose file that hold something. Compose merges
// the documents of one file in order, as it would separate files, so each is a full compose
// document of its own. A document that holds nothing (empty, whitespace or comments only, or a
// bare null) is skipped. One whose root is not a mapping is an error: skipping it would make
// Resolve and Analyze silently report "nothing to do" for a file Compose is going to reject.
func parseAll(src []byte) ([]*yamlv3.Node, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(src))
	var docs []*yamlv3.Node
	for i := 1; ; i++ {
		doc := &yamlv3.Node{}
		err := dec.Decode(doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse compose override: %w", err)
		}
		if doc.Kind != yamlv3.DocumentNode || len(doc.Content) == 0 || isNull(doc.Content[0]) {
			continue
		}
		if doc.Content[0].Kind != yamlv3.MappingNode {
			where := ""
			if i > 1 {
				where = fmt.Sprintf(" (document %d)", i)
			}
			return nil, fmt.Errorf("parse compose override: the top level must be a mapping%s, got %s", where, kindName(doc.Content[0]))
		}
		docs = append(docs, doc)
	}
}

// isNull reports whether n is a plain null scalar, which is what the decoder returns for a
// document holding only comments.
func isNull(n *yamlv3.Node) bool {
	return n.Kind == yamlv3.ScalarNode && n.ShortTag() == "!!null"
}

// isSet reports whether a key's value actually sets something. A missing key, a null value
// and a `!reset` tag (whatever follows it) all leave the attribute at its default, so for
// Compose the service has no image or build at all.
func isSet(n *yamlv3.Node) bool {
	return n != nil && n.Tag != "!reset" && !isNull(n)
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
	err     error // the first entry that could not be rewritten; Resolve returns it
}

func (r *resolver) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *resolver) document(doc *yamlv3.Node) {
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

func (r *resolver) service(svc *yamlv3.Node) {
	if svc == nil || svc.Kind != yamlv3.MappingNode {
		return
	}
	if vols := mapGet(svc, "volumes"); vols != nil && vols.Kind == yamlv3.SequenceNode {
		for _, v := range vols.Content {
			r.volume(deref(v))
		}
	}
	if b := mapGet(svc, "build"); isSet(b) {
		switch b.Kind {
		case yamlv3.ScalarNode:
			r.buildContext(b)
		case yamlv3.MappingNode:
			r.buildMapping(b)
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
	// develop.watch[].target is a container path and ignore patterns are relative to path, so
	// only path is a host path.
	if watch := mapGet(mapGet(svc, "develop"), "watch"); watch != nil && watch.Kind == yamlv3.SequenceNode {
		for _, item := range watch.Content {
			r.scalar(mapGet(deref(item), "path"))
		}
	}
}

// buildMapping rewrites the host paths of a long-syntax build. build.dockerfile is relative to
// the context, so it stays as written.
func (r *resolver) buildMapping(b *yamlv3.Node) {
	if ctx := mapGet(b, "context"); ctx != nil {
		r.buildContext(ctx)
	} else {
		// Compose defaults the context to ".", which it would read as the internal directory.
		// Spelling out the project directory keeps `build: {dockerfile: x}` building the project.
		b.Content = append([]*yamlv3.Node{
			{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: "context"},
			{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: escapeDollar(r.baseDir)},
		}, b.Content...)
	}

	// additional_contexts is a mapping of name: context or a list of name=context.
	ac := mapGet(b, "additional_contexts")
	if ac != nil && ac.Kind == yamlv3.MappingNode {
		mapEntries(ac, func(_ string, v *yamlv3.Node) {
			if v != nil && v.Kind == yamlv3.ScalarNode && !isNonPathContext(v.Value) {
				r.scalar(v)
			}
		})
	}
	if ac != nil && ac.Kind == yamlv3.SequenceNode {
		for _, item := range ac.Content {
			r.keyValue(deref(item), func(v string) bool { return !isNonPathContext(v) })
		}
	}

	// ssh entries are `id` (the agent socket, no path) or `id=path` to a key file.
	ssh := mapGet(b, "ssh")
	if ssh != nil && ssh.Kind == yamlv3.SequenceNode {
		for _, item := range ssh.Content {
			r.keyValue(deref(item), func(string) bool { return true })
		}
	}
	if ssh != nil && ssh.Kind == yamlv3.MappingNode {
		mapEntries(ssh, func(_ string, v *yamlv3.Node) { r.scalar(v) })
	}
}

// keyValue rewrites the value of a `key=value` scalar when isPath accepts it. An entry
// without "=" carries no path and is left alone.
func (r *resolver) keyValue(n *yamlv3.Node, isPath func(string) bool) {
	if n == nil || n.Kind != yamlv3.ScalarNode {
		return
	}
	k, v, found := strings.Cut(n.Value, "=")
	if !found || !isPath(v) {
		return
	}
	n.Value = k + "=" + r.rewrite(v, true)
}

func (r *resolver) include(item *yamlv3.Node) {
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
func (r *resolver) scalarOrList(n *yamlv3.Node, fn func(*yamlv3.Node)) {
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
func (r *resolver) volume(v *yamlv3.Node) {
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
func (r *resolver) shortVolume(n *yamlv3.Node) {
	src, rest, found := strings.Cut(n.Value, ":")
	if !found || !isRelative(src) {
		return
	}
	abs := r.rewrite(src, false)
	if !strings.Contains(abs, ":") {
		n.Value = abs + ":" + rest
		return
	}
	// The directory the path was made absolute against contains a ":", and Compose would split
	// the short syntax there. Only the long syntax can carry such a source.
	dir := r.baseDir
	if strings.HasPrefix(src, "~") {
		dir = r.home
	}
	r.longVolume(n, abs, rest, dir)
}

// longVolume turns the short-syntax entry n into the equivalent long-syntax mapping, in place
// so an alias to the entry sees the change too. create_host_path is set because the short
// syntax implies it: without it a missing source would stop the container from starting
// instead of being created as before.
func (r *resolver) longVolume(n *yamlv3.Node, source, rest, dir string) {
	target, mode, _ := strings.Cut(rest, ":")
	if target == "" {
		r.fail(fmt.Errorf("volume %q: %s contains \":\", so the entry must be written in long syntax, and it has no target", n.Value, dir))
		return
	}

	str := func(v string) *yamlv3.Node { return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: v} }
	pair := func(k string, v *yamlv3.Node) []*yamlv3.Node { return []*yamlv3.Node{str(k), v} }
	yes := &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!bool", Value: "true"}

	content := append(pair("type", str("bind")), pair("source", str(source))...)
	content = append(content, pair("target", str(target))...)
	bind := pair("create_host_path", yes)
	for _, opt := range strings.Split(mode, ",") {
		switch opt {
		case "", "rw":
		case "ro":
			content = append(content, pair("read_only", yes)...)
		case "z", "Z":
			bind = append(bind, pair("selinux", str(opt))...)
		case "shared", "rshared", "slave", "rslave", "private", "rprivate":
			bind = append(bind, pair("propagation", str(opt))...)
		case "cached", "delegated", "consistent":
			content = append(content, pair("consistency", str(opt))...)
		default:
			r.fail(fmt.Errorf("volume %q: %s contains \":\", so the entry must be written in long syntax, "+
				"and Orobox cannot convert the %q mode; write this volume in long syntax yourself", n.Value, dir, opt))
			return
		}
	}
	content = append(content, pair("bind", &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map", Content: bind})...)

	n.Kind = yamlv3.MappingNode
	n.Tag = "!!map"
	n.Value = ""
	n.Style = 0
	n.Content = content
}

// buildContext rewrites a build context, leaving remote contexts (git URLs and the like)
// alone since they are not host paths.
func (r *resolver) buildContext(n *yamlv3.Node) {
	if n == nil || n.Kind != yamlv3.ScalarNode || isRemoteContext(n.Value) {
		return
	}
	r.scalar(n)
}

// isRemoteContext reports whether a build context is fetched rather than read from the host:
// any URL (git or tarball) and the two scp-like and bare-host git spellings Docker accepts.
func isRemoteContext(p string) bool {
	return strings.Contains(p, "://") || strings.HasPrefix(p, "git@") || strings.HasPrefix(p, "github.com/")
}

// isNonPathContext reports whether an additional build context names something other than a
// host directory: a remote context, an image, another service's image or a build stage.
func isNonPathContext(p string) bool {
	return isRemoteContext(p) || strings.HasPrefix(p, "service:") || strings.HasPrefix(p, "target:")
}

// scalar rewrites a scalar node that holds a path for a key where Compose resolves a bare
// name such as `foo.env` against the project directory too.
func (r *resolver) scalar(n *yamlv3.Node) {
	if n == nil || n.Kind != yamlv3.ScalarNode {
		return
	}
	n.Value = r.rewrite(n.Value, true)
}

// rewrite turns p into an absolute path. With bare set, a name that starts with neither "."
// nor "~" counts as relative as well. Absolute paths, "$"-prefixed values (interpolation is
// the user's responsibility) and the empty string are returned unchanged, as is a "~" when
// home is unknown.
//
// A "$" in baseDir or home is escaped as "$$": Compose interpolates the whole value, and the
// directory is a literal. The part the user wrote is kept as is, interpolation included.
func (r *resolver) rewrite(p string, bare bool) string {
	if p == "" || strings.HasPrefix(p, "$") || filepath.IsAbs(p) {
		return p
	}
	if !bare && !isRelative(p) {
		return p
	}
	if strings.HasPrefix(p, "~") {
		if r.home == "" {
			return p
		}
		// Compose expands any leading "~" this way, so "~user/x" is home + "user/x" and not
		// another user's home directory.
		return filepath.Join(escapeDollar(r.home), p[1:])
	}
	return filepath.Join(escapeDollar(r.baseDir), p)
}

// escapeDollar escapes "$" the way Compose expects a literal one.
func escapeDollar(p string) string {
	return strings.ReplaceAll(p, "$", "$$")
}

// isRelative reports whether a short-syntax volume source is a host path. This is the same
// rule Compose uses to tell a path from a named volume.
func isRelative(p string) bool {
	return strings.HasPrefix(p, ".") || strings.HasPrefix(p, "~")
}
