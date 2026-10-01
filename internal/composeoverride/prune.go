package composeoverride

import (
	"sort"

	yamlv3 "gopkg.in/yaml.v3"
)

// ServiceNames returns the names of the services a compose file defines, across all of its
// documents, sorted and without repeats. Orobox reads its own generated files with it to learn
// which services an override may extend.
func ServiceNames(src []byte) ([]string, error) {
	docs, err := parseAll(src)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, doc := range docs {
		mapEntries(mapGet(doc.Content[0], "services"), func(name string, _ *yamlv3.Node) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		})
	}
	sort.Strings(names)
	return names, nil
}

// Prune removes from a resolved override the services that only make sense on top of a
// service the stack does not define: not in known, and with no image, build or extends of
// their own in any document. Compose rejects the whole project over one such service ("has
// neither an image nor a build context"), so an override tweaking db-test, which exists only
// for test commands, or a disabled optional service would otherwise break every command.
//
// dropped lists the removed services in the order they first appear. With nothing to drop the
// input is returned unchanged; an empty file returns (nil, nil, nil).
//
// Only a service's own entry under `services` is removed. One contributed to the services
// mapping through a merge key cannot be taken out without rewriting the anchor, and is kept.
func Prune(resolved []byte, known []string) (out []byte, dropped []string, err error) {
	docs, err := parseAll(resolved)
	if err != nil || len(docs) == 0 {
		return nil, nil, err
	}

	isKnown := map[string]bool{}
	for _, name := range known {
		isKnown[name] = true
	}
	standalone := map[string]bool{}
	var order []string
	seen := map[string]bool{}
	for _, doc := range docs {
		mapEntries(mapGet(doc.Content[0], "services"), func(name string, svc *yamlv3.Node) {
			if !seen[name] {
				seen[name] = true
				order = append(order, name)
			}
			if isSet(mapGet(svc, "image")) || isSet(mapGet(svc, "build")) || isSet(mapGet(svc, "extends")) {
				standalone[name] = true
			}
		})
	}
	drop := map[string]bool{}
	for _, name := range order {
		if !isKnown[name] && !standalone[name] {
			drop[name] = true
			dropped = append(dropped, name)
		}
	}
	if len(dropped) == 0 {
		return resolved, nil, nil
	}

	for _, doc := range docs {
		dropServices(doc, drop)
	}
	out, err = encodeAll(docs)
	if err != nil {
		return nil, nil, err
	}
	return out, dropped, nil
}

// dropServices removes the drop services from doc's own services mapping. An alias elsewhere
// in the document may point into a removed entry (`<<: *db-test`); it is replaced by a copy of
// what it pointed at, since the anchor leaves the file with the entry.
func dropServices(doc *yamlv3.Node, drop map[string]bool) {
	services := deref(ownValue(doc.Content[0], "services"))
	if services == nil || services.Kind != yamlv3.MappingNode {
		return
	}

	removed := map[*yamlv3.Node]bool{}
	var kept []*yamlv3.Node
	for i := 0; i+1 < len(services.Content); i += 2 {
		k, v := services.Content[i], services.Content[i+1]
		if !isMergeKey(k) && drop[k.Value] {
			markSubtree(v, removed)
			continue
		}
		kept = append(kept, k, v)
	}
	services.Content = kept
	expandAliasesInto(doc, removed, 0)
}

// ownValue returns the value a mapping stores under key itself, merge keys not followed.
func ownValue(m *yamlv3.Node, key string) *yamlv3.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && !isMergeKey(m.Content[i]) {
			return m.Content[i+1]
		}
	}
	return nil
}

// markSubtree records n and every node below it. Aliases are recorded but not followed: what
// they point at is defined, and stays, elsewhere.
func markSubtree(n *yamlv3.Node, set map[*yamlv3.Node]bool) {
	if n == nil || set[n] {
		return
	}
	set[n] = true
	if n.Kind == yamlv3.AliasNode {
		return
	}
	for _, c := range n.Content {
		markSubtree(c, set)
	}
}

// expandAliasesInto replaces, below n, every alias whose target is in removed by a copy of
// that target, keeping the alias's own comments.
func expandAliasesInto(n *yamlv3.Node, removed map[*yamlv3.Node]bool, depth int) {
	if n == nil || depth > maxDepth*maxDepth {
		return
	}
	if n.Kind == yamlv3.AliasNode {
		if removed[n.Alias] {
			head, line, foot := n.HeadComment, n.LineComment, n.FootComment
			*n = *copyExpanded(n.Alias, removed, 0)
			n.HeadComment, n.LineComment, n.FootComment = head, line, foot
		}
		return
	}
	for _, c := range n.Content {
		expandAliasesInto(c, removed, depth+1)
	}
}

// copyExpanded deep-copies n without anchors (they would be duplicates of ones that are gone
// anyway), expanding aliases into removed nodes as it goes. Aliases to nodes that stay are
// kept as aliases.
func copyExpanded(n *yamlv3.Node, removed map[*yamlv3.Node]bool, depth int) *yamlv3.Node {
	if n.Kind == yamlv3.AliasNode {
		if removed[n.Alias] && depth < maxDepth {
			return copyExpanded(n.Alias, removed, depth+1)
		}
		c := *n
		return &c
	}
	c := *n
	c.Anchor = ""
	c.HeadComment, c.LineComment, c.FootComment = "", "", ""
	c.Content = make([]*yamlv3.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = copyExpanded(child, removed, depth+1)
	}
	return &c
}
