package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Psr4Target is where a project's composer.json says a namespace's files belong.
type Psr4Target struct {
	// Prefix is the matched PSR-4 namespace prefix. An OroCommerce application maps the
	// empty prefix, so this is usually "".
	Prefix string
	// Dir is the source root the prefix maps to, relative to the project root — "src" for
	// the `"": "src/"` rule every OroCommerce application skeleton ships.
	Dir string
	// RelPath is Dir joined with the part of the namespace below Prefix: the directory the
	// bundle's own files go in, relative to the project root.
	RelPath string
}

// ErrNoPsr4Root reports that a directory is not a PHP project that can autoload a generated
// bundle: it has no composer.json, or none of its PSR-4 prefixes covers the namespace.
var ErrNoPsr4Root = errors.New("no composer.json PSR-4 prefix covers this namespace")

// ResolvePsr4Dir maps a PHP namespace onto a directory using the PSR-4 map in
// <projectRoot>/composer.json.
//
// This is what makes `orobox create bundle` land in the right place inside an OroCommerce
// checkout without being told: the application skeleton autoloads `"": "src/"`, so
// `Acme\Bundle\FooBundle` resolves to `src/Acme/Bundle/FooBundle` and is autoloaded and
// discovered by Oro's kernel with no composer.json of its own.
//
// The longest matching prefix wins, which is PSR-4's own rule: a project that maps both
// `""` and `Acme\` sends `Acme\Bundle\FooBundle` to the `Acme\` root.
func ResolvePsr4Dir(projectRoot, namespace string) (Psr4Target, error) {
	raw, err := os.ReadFile(filepath.Join(projectRoot, "composer.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Psr4Target{}, ErrNoPsr4Root
		}
		return Psr4Target{}, fmt.Errorf("could not read composer.json in %s: %w", projectRoot, err)
	}

	var doc struct {
		Autoload struct {
			Psr4 map[string]json.RawMessage `json:"psr-4"`
		} `json:"autoload"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Psr4Target{}, fmt.Errorf("could not parse composer.json in %s: %w", projectRoot, err)
	}
	if len(doc.Autoload.Psr4) == 0 {
		return Psr4Target{}, ErrNoPsr4Root
	}

	// Longest prefix first, so `Acme\` beats `""` for `Acme\Bundle\FooBundle`. Ties are
	// broken alphabetically only to keep the result stable across runs.
	prefixes := make([]string, 0, len(doc.Autoload.Psr4))
	for prefix := range doc.Autoload.Psr4 {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if len(prefixes[i]) != len(prefixes[j]) {
			return len(prefixes[i]) > len(prefixes[j])
		}
		return prefixes[i] < prefixes[j]
	})

	namespace = strings.Trim(namespace, `\`)
	for _, prefix := range prefixes {
		trimmed := strings.Trim(prefix, `\`)
		if trimmed != "" && !strings.HasPrefix(namespace+`\`, trimmed+`\`) {
			continue
		}

		dir, err := firstPsr4Dir(doc.Autoload.Psr4[prefix])
		if err != nil {
			return Psr4Target{}, fmt.Errorf("composer.json PSR-4 entry %q in %s: %w", prefix, projectRoot, err)
		}
		if dir == "" {
			// A prefix mapped to the package root cannot host a subdirectory tree without
			// colliding with the package's own files; fall through to the next prefix.
			continue
		}

		rest := strings.TrimPrefix(strings.TrimPrefix(namespace, trimmed), `\`)
		relPath := filepath.Clean(dir)
		if rest != "" {
			relPath = filepath.Join(relPath, filepath.Join(strings.Split(rest, `\`)...))
		}
		return Psr4Target{Prefix: prefix, Dir: filepath.Clean(dir), RelPath: relPath}, nil
	}

	return Psr4Target{}, ErrNoPsr4Root
}

// firstPsr4Dir reads one PSR-4 value, which composer allows to be either a single directory or
// a list of them. The first entry wins: composer searches them in order, so that is the one a
// newly generated class is found in.
func firstPsr4Dir(value json.RawMessage) (string, error) {
	var single string
	if err := json.Unmarshal(value, &single); err == nil {
		return strings.TrimSuffix(single, "/"), nil
	}

	var many []string
	if err := json.Unmarshal(value, &many); err != nil {
		return "", errors.New("value is neither a directory nor a list of directories")
	}
	if len(many) == 0 {
		return "", errors.New("value is an empty list")
	}
	return strings.TrimSuffix(many[0], "/"), nil
}
