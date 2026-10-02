// Package yamledit makes small, targeted edits to YAML files (.orobox.yaml,
// .orobox.compose.yaml) without destroying what the user wrote around them.
//
// Round-tripping through map[string]any would drop every comment and reorder
// keys, which is unacceptable for files people hand-edit. Instead the document
// is kept as a yaml.v3 node tree, so values, comments, key order and the
// flow/block style of untouched content survive an edit. The file is still
// re-emitted whole by the encoder, which normalizes what the node tree does not
// record: blank lines are dropped, indentation becomes two spaces, CRLF line
// endings become LF and long plain scalars may be refolded.
// It backs `orobox extend` and the recipes, which add keys, list items and
// compose services to an existing file.
//
// Path segments are mapping keys only; there is no list indexing because no
// caller needs it.
package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	tagStr  = "!!str"
	tagBool = "!!bool"
	tagNull = "!!null"
	tagMap  = "!!map"
	tagSeq  = "!!seq"
)

// Doc is a parsed YAML document whose root is always a mapping.
type Doc struct {
	doc *yaml.Node // DocumentNode wrapping the root mapping
}

// Parse reads src into an editable document. Empty, whitespace-only and
// comments-only input yield an empty mapping (comments are kept) so a caller
// can start from a file that does not exist yet. A root that is not a mapping
// is rejected: every file orobox edits is a mapping at the top level.
func Parse(src []byte) (*Doc, error) {
	var n yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(src))
	if err := dec.Decode(&n); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	// Bytes writes back one document, so a second one would be silently deleted from the
	// user's file. Refusing is the only edit that cannot lose data.
	// A trailing `---` with nothing (or only comments) after it is not a second document worth
	// keeping; anything with content is.
	for {
		var extra yaml.Node
		err := dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse yaml: %w", err)
		}
		if !emptyDocument(&extra) {
			return nil, errors.New("parse yaml: the file holds several YAML documents (separated by ---); merge them into one before orobox edits it")
		}
	}
	if n.Kind == 0 {
		// yaml.v3 reports "no document" for input without content and drops any
		// comments it contained, so carry them over by hand.
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
		doc.HeadComment = commentsOnly(src)
		return &Doc{doc: doc}, nil
	}
	if n.Kind != yaml.DocumentNode || len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("parse yaml: top level must be a mapping")
	}
	return &Doc{doc: &n}, nil
}

// commentsOnly returns src trimmed when every non-blank line is a comment, and
// "" otherwise (for instance a lone "---" marker, which has nothing to keep).
func commentsOnly(src []byte) string {
	var lines []string
	for _, l := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			return ""
		}
		lines = append(lines, t)
	}
	return strings.Join(lines, "\n")
}

// Bytes serializes the document with a 2-space indent, comments kept.
func (d *Doc) Bytes() ([]byte, error) {
	clearMergeTags(d.doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d.doc); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// Has reports whether path exists. Aliases are followed while reading.
func (d *Doc) Has(path []string) bool {
	return d.lookup(path) != nil
}

// Kind returns the kind of the node at path (aliases dereferenced), or 0 when
// the path is absent.
func (d *Doc) Kind(path []string) yaml.Kind {
	if n := d.lookup(path); n != nil {
		return n.Kind
	}
	return 0
}

// SetScalar sets the string value at path, creating the key and any missing
// intermediate mappings. An existing scalar keeps its comments and style; an
// existing non-scalar value is an error rather than being silently replaced.
func (d *Doc) SetScalar(path []string, value string) error {
	// Force string semantics: without an explicit !!str, a value like "true" or "8080" would be
	// written plain and read back as a bool/int.
	return d.setScalar(path, value, tagStr)
}

// SetBool sets the scalar at path to a YAML boolean, with the same rules as SetScalar. It exists
// because SetScalar always writes a string, and a quoted "true" does not decode into a Go bool.
func (d *Doc) SetBool(path []string, value bool) error {
	text := "false"
	if value {
		text = "true"
	}
	return d.setScalar(path, text, tagBool)
}

func (d *Doc) setScalar(path []string, value, tag string) error {
	if len(path) == 0 {
		return errors.New("set scalar: empty path")
	}
	parent, err := d.walk(path[:len(path)-1])
	if err != nil {
		return err
	}
	key := path[len(path)-1]
	if _, v := mapGet(parent, key); v != nil {
		if v.Kind != yaml.ScalarNode {
			return fmt.Errorf("%s: cannot set a scalar over an existing %s", joinPath(path), kindName(v.Kind))
		}
		v.Value = value
		v.Tag = tag
		v.Style &^= yaml.TaggedStyle
		// A replaced value is written plain; a previously quoted "no" must not stay quoted
		// once it becomes a boolean.
		if tag != tagStr {
			v.Style &^= yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
		}
		return nil
	}
	mapSet(parent, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
	return nil
}

// AppendUnique appends each value to the sequence at path unless it is already
// present (values repeated within the call are added once). A missing key is
// created as a sequence; an empty value (`key:`) is filled in. The flow/block
// style and comments of an existing sequence are preserved.
func (d *Doc) AppendUnique(path []string, values ...string) error {
	if len(path) == 0 {
		return errors.New("append: empty path")
	}
	parent, err := d.walk(path[:len(path)-1])
	if err != nil {
		return err
	}
	key := path[len(path)-1]
	_, seq := mapGet(parent, key)
	switch {
	case seq == nil:
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: tagSeq}
		mapSet(parent, key, seq)
	case isNull(seq):
		toKind(seq, yaml.SequenceNode, tagSeq)
		keepLineComment(parent, key, seq)
	case seq.Kind != yaml.SequenceNode:
		return fmt.Errorf("%s: cannot append to an existing %s", joinPath(path), kindName(seq.Kind))
	}

	seen := make(map[string]bool, len(seq.Content)+len(values))
	for _, it := range seq.Content {
		if it = deref(it); it.Kind == yaml.ScalarNode {
			seen[it.Value] = true
		}
	}
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		seq.Content = append(seq.Content, strScalar(v))
	}
	return nil
}

// MergeMapping merges the keys of fragment (a mapping) into the mapping at
// path, creating it when missing. Keys absent from the target are added as deep
// copies. Keys already present are left alone and reported in skipped, unless
// overwrite is set, in which case their value is replaced and the key is
// reported in added (it was written). Both lists follow fragment order.
func (d *Doc) MergeMapping(path []string, fragment *yaml.Node, overwrite bool) (added, skipped []string, err error) {
	if fragment != nil && fragment.Kind == yaml.DocumentNode && len(fragment.Content) == 1 {
		// yaml.Unmarshal into a Node yields a document wrapper; accept it so
		// callers can pass their parse result straight in.
		fragment = fragment.Content[0]
	}
	if fragment == nil || fragment.Kind != yaml.MappingNode {
		return nil, nil, errors.New("merge: fragment must be a mapping")
	}
	// Every key is checked before the first change (walk below may already create the target),
	// so a bad fragment leaves the document as it was instead of half merged.
	for i := 0; i+1 < len(fragment.Content); i += 2 {
		if k := fragment.Content[i]; k.Kind != yaml.ScalarNode {
			return nil, nil, fmt.Errorf("merge: fragment key at line %d is not a scalar", k.Line)
		}
	}
	target, err := d.walk(path)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i+1 < len(fragment.Content); i += 2 {
		k, v := fragment.Content[i], fragment.Content[i+1]
		_, existing := mapGet(target, k.Value)
		switch {
		case existing == nil:
			target.Content = append(target.Content, cloneNode(k, 0), cloneNode(v, 0))
			added = append(added, k.Value)
		case overwrite:
			for j := 0; j+1 < len(target.Content); j += 2 {
				if target.Content[j+1] == existing {
					target.Content[j+1] = cloneNode(v, 0)
					break
				}
			}
			added = append(added, k.Value)
		default:
			skipped = append(skipped, k.Value)
		}
	}
	return added, skipped, nil
}

// lookup resolves path for reading: aliases are followed at every step and the
// result is the dereferenced node, or nil when absent (including when an
// intermediate is not a mapping).
func (d *Doc) lookup(path []string) *yaml.Node {
	cur := deref(d.doc.Content[0])
	for _, seg := range path {
		if cur.Kind != yaml.MappingNode {
			return nil
		}
		_, v := mapGet(cur, seg)
		if v == nil {
			return nil
		}
		cur = deref(v)
	}
	return cur
}

// walk resolves path for writing and returns the mapping at it, creating
// missing mappings along the way (an empty `key:` is turned into one). Unlike
// reading it does not follow aliases: editing through one would silently change
// the anchored node and every other alias of it.
func (d *Doc) walk(path []string) (*yaml.Node, error) {
	cur := d.doc.Content[0]
	for i, seg := range path {
		_, next := mapGet(cur, seg)
		switch {
		case next == nil:
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap}
			mapSet(cur, seg, next)
		case isNull(next):
			toKind(next, yaml.MappingNode, tagMap)
			keepLineComment(cur, seg, next)
		case next.Kind == yaml.AliasNode:
			return nil, fmt.Errorf("%s: refusing to edit through an alias", joinPath(path[:i+1]))
		case next.Kind != yaml.MappingNode:
			return nil, fmt.Errorf("%s: expected a mapping, found %s", joinPath(path[:i+1]), kindName(next.Kind))
		}
		cur = next
	}
	if cur.Kind == yaml.MappingNode && len(cur.Content) == 0 {
		// Filling an empty `{}` would otherwise stay in flow style
		// (`services: {a: ...}`), which is unreadable for compose services.
		cur.Style &^= yaml.FlowStyle
	}
	return cur, nil
}

// mapGet finds an own key of m (exact match on scalar keys; `<<` merge keys are
// not expanded, so inherited keys count as absent). It returns the key and
// value nodes, or nils.
func mapGet(m *yaml.Node, key string) (k, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if kn := m.Content[i]; kn.Kind == yaml.ScalarNode && kn.Value == key {
			return kn, m.Content[i+1]
		}
	}
	return nil, nil
}

func mapSet(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: key}, value)
}

func strScalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: v}
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == tagNull
}

// emptyDocument reports whether a decoded document holds no content: a bare `---`, or one
// followed only by comments.
func emptyDocument(n *yaml.Node) bool {
	if n.Kind == 0 {
		return true
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		c := n.Content[0]
		return c.Kind == yaml.ScalarNode && c.Tag == tagNull && c.Value == ""
	}
	return false
}

// keepLineComment moves the line comment of a value that just became a
// collection onto its key: `image: ~ # note` would otherwise lose the note,
// because the encoder has nowhere to put a line comment on a block mapping.
func keepLineComment(parent *yaml.Node, key string, value *yaml.Node) {
	if value.LineComment == "" {
		return
	}
	if k, _ := mapGet(parent, key); k != nil && k.LineComment == "" {
		k.LineComment, value.LineComment = value.LineComment, ""
	}
}

// clearMergeTags drops the explicit !!merge tag the decoder puts on `<<` keys,
// which the encoder would otherwise print as `!!merge <<:`.
func clearMergeTags(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == "!!merge" {
		n.Tag = ""
	}
	for _, c := range n.Content {
		clearMergeTags(c)
	}
}

// toKind turns n into an empty node of another kind in place, so comments
// attached to it are kept.
func toKind(n *yaml.Node, kind yaml.Kind, tag string) {
	n.Kind, n.Tag, n.Value, n.Style, n.Content = kind, tag, "", 0, nil
}

// cloneNode deep-copies n with every alias expanded into a copy of what it
// points at, and without anchor names. An alias kept in the copy could point
// at an anchor outside it (written as an undefined `*name`), and a copied
// anchor name could re-bind one the user's document already uses. The depth
// bound stops a cyclic alias from recursing forever.
func cloneNode(n *yaml.Node, depth int) *yaml.Node {
	if n == nil {
		return nil
	}
	if depth > maxCloneDepth {
		// Only a cyclic alias gets here. A null keeps the parent's key/value pairs aligned;
		// dropping the child would shift every later pair and corrupt the mapping.
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagNull}
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		return cloneNode(n.Alias, depth+1)
	}
	c := *n
	c.Anchor = ""
	c.Alias = nil
	c.Content = nil
	for _, ch := range n.Content {
		c.Content = append(c.Content, cloneNode(ch, depth+1))
	}
	return &c
}

// maxCloneDepth bounds cloneNode; real fragments are a handful of levels deep.
const maxCloneDepth = 64

func joinPath(path []string) string {
	return strings.Join(path, ".")
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return "node"
}
