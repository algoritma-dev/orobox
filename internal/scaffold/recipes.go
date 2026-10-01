package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/yamledit"

	yamlv3 "gopkg.in/yaml.v3"
)

const (
	// recipesDir holds one directory per recipe, relative to the templates root.
	recipesDir = "recipes"
	// recipeManifest is the file in a recipe directory that says what the recipe merges.
	recipeManifest = "recipe.yaml"
	// recipeFilesDir is the optional directory of a recipe whose contents land in docker/<name>/.
	recipeFilesDir = "files"
	// projectEnvFile is the project's sparse .env, merged over the generated one (D7).
	projectEnvFile = ".env"
)

// Recipe is a ready-made addition to the stack: a compose fragment and, optionally, the config
// keys, .env keys and files the service needs. It is plain data read from
// templates/recipes/<name>/, so adding one is a new directory and a test case, with no Go code.
type Recipe struct {
	Name, Description, Notes string
	// Compose is a mapping (services, volumes, ...) merged into .orobox.compose.yaml.
	Compose *yamlv3.Node
	// Config is an optional mapping merged into .orobox.yaml: lists are appended without
	// duplicates, scalars only set when the project does not have them.
	Config *yamlv3.Node
	// Env holds optional keys appended to the project .env when it does not define them.
	Env map[string]string
	// Files is copied to docker/<Name>/ in the project, never overwriting a file.
	Files fs.FS
}

// recipeDoc is the on-disk shape of recipe.yaml. The compose and config sections stay node trees:
// they are merged into the user's files node by node, so their comments and tags carry over.
type recipeDoc struct {
	Description string            `yaml:"description"`
	Notes       string            `yaml:"notes"`
	Compose     yamlv3.Node       `yaml:"compose"`
	Config      yamlv3.Node       `yaml:"config"`
	Env         map[string]string `yaml:"env"`
}

// EmbeddedRecipes loads the recipes shipped in the binary.
func EmbeddedRecipes() ([]Recipe, error) {
	if Templates == nil {
		return nil, errors.New("recipes: templates are not loaded")
	}
	root, err := fs.Sub(Templates, "templates")
	if err != nil {
		return nil, fmt.Errorf("recipes: %w", err)
	}
	return LoadRecipes(root)
}

// LoadRecipes reads every recipe under recipes/ in fsys, sorted by name. A malformed recipe fails
// the whole load: recipes ship with the binary, so a broken one is a bug to catch in tests, not
// something to skip at run time.
func LoadRecipes(fsys fs.FS) ([]Recipe, error) {
	entries, err := fs.ReadDir(fsys, recipesDir)
	if err != nil {
		return nil, fmt.Errorf("recipes: %w", err)
	}
	var recipes []Recipe
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loadRecipe(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, r)
	}
	// fs.ReadDir already sorts by file name; sorting again keeps the contract independent of
	// the FS implementation.
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].Name < recipes[j].Name })
	return recipes, nil
}

func loadRecipe(fsys fs.FS, name string) (Recipe, error) {
	dir := path.Join(recipesDir, name)
	raw, err := fs.ReadFile(fsys, path.Join(dir, recipeManifest))
	if err != nil {
		return Recipe{}, fmt.Errorf("recipe %s: %w", name, err)
	}

	var doc recipeDoc
	dec := yamlv3.NewDecoder(bytes.NewReader(raw))
	// A misspelled section would otherwise be ignored and the recipe would silently add less.
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return Recipe{}, fmt.Errorf("recipe %s: %s: %w", name, recipeManifest, err)
	}

	r := Recipe{
		Name:        name,
		Description: strings.TrimSpace(doc.Description),
		Notes:       strings.TrimRight(doc.Notes, "\n"),
		Env:         doc.Env,
	}
	if r.Description == "" {
		return Recipe{}, fmt.Errorf("recipe %s: description is required", name)
	}
	if doc.Compose.Kind != yamlv3.MappingNode {
		return Recipe{}, fmt.Errorf("recipe %s: compose must be a mapping", name)
	}
	if !hasMappingKey(&doc.Compose, "services") {
		return Recipe{}, fmt.Errorf("recipe %s: compose.services is required", name)
	}
	r.Compose = &doc.Compose
	switch {
	case doc.Config.Kind == 0, doc.Config.Kind == yamlv3.ScalarNode && doc.Config.Tag == "!!null":
	case doc.Config.Kind == yamlv3.MappingNode:
		r.Config = &doc.Config
	default:
		return Recipe{}, fmt.Errorf("recipe %s: config must be a mapping", name)
	}

	filesDir := path.Join(dir, recipeFilesDir)
	if info, err := fs.Stat(fsys, filesDir); err == nil && info.IsDir() {
		if r.Files, err = fs.Sub(fsys, filesDir); err != nil {
			return Recipe{}, fmt.Errorf("recipe %s: %w", name, err)
		}
	}
	return r, nil
}

// plannedWrite is one file AddRecipe is about to touch. Content is nil when the file is left alone.
type plannedWrite struct {
	rel     string // project-relative, forward slashes
	content []byte
	action  string
}

// AddRecipe merges a recipe into the project: its compose fragment into .orobox.compose.yaml, its
// config into .orobox.yaml, its env keys into .env and its files into docker/<name>/.
//
// A project value always wins over a recipe value, and a service the override already defines is
// refused unless force is set, in which case only the recipe's own services are replaced. Every
// change is computed before the first file is written, so a refused recipe leaves the project
// exactly as it was.
func AddRecipe(projectDir string, r Recipe, force bool) ([]Receipt, error) {
	compose, err := planRecipeCompose(projectDir, r, force)
	if err != nil {
		return nil, err
	}
	plan := []plannedWrite{compose}

	if r.Config != nil {
		cfg, err := planRecipeConfig(projectDir, r)
		if err != nil {
			return nil, err
		}
		plan = append(plan, cfg)
	}
	if len(r.Env) > 0 {
		env, err := planRecipeEnv(projectDir, r)
		if err != nil {
			return nil, err
		}
		plan = append(plan, env)
	}
	if r.Files != nil {
		files, err := planRecipeFiles(projectDir, r)
		if err != nil {
			return nil, err
		}
		plan = append(plan, files...)
	}

	var receipts []Receipt
	for _, w := range plan {
		if w.content != nil {
			target := filepath.Join(projectDir, filepath.FromSlash(w.rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return receipts, fmt.Errorf("could not create the directory of %s: %w", w.rel, err)
			}
			if err := os.WriteFile(target, w.content, 0o644); err != nil {
				return receipts, fmt.Errorf("could not write %s: %w", w.rel, err)
			}
		}
		receipts = append(receipts, Receipt{Path: w.rel, Action: w.action})
	}
	return receipts, nil
}

// planRecipeCompose merges the compose fragment into the team override, creating it when missing.
func planRecipeCompose(projectDir string, r Recipe, force bool) (plannedWrite, error) {
	src, exists, err := readOptional(filepath.Join(projectDir, ComposeOverrideFile))
	if err != nil {
		return plannedWrite{}, err
	}
	doc, err := yamledit.Parse(src)
	if err != nil {
		return plannedWrite{}, fmt.Errorf("%s: %w", ComposeOverrideFile, err)
	}
	before, err := doc.Bytes()
	if err != nil {
		return plannedWrite{}, err
	}

	if !force {
		var taken []string
		for _, name := range mappingKeys(mappingValue(r.Compose, "services")) {
			if doc.Has([]string{"services", name}) {
				taken = append(taken, name)
			}
		}
		switch len(taken) {
		case 0:
		case 1:
			return plannedWrite{}, fmt.Errorf("%s already defines the service %s; use --force to replace it with the recipe's",
				ComposeOverrideFile, taken[0])
		default:
			return plannedWrite{}, fmt.Errorf("%s already defines the services %s; use --force to replace them with the recipe's",
				ComposeOverrideFile, strings.Join(taken, ", "))
		}
	}

	for i := 0; i+1 < len(r.Compose.Content); i += 2 {
		key, value := r.Compose.Content[i].Value, r.Compose.Content[i+1]
		if value.Kind != yamlv3.MappingNode {
			return plannedWrite{}, fmt.Errorf("recipe %s: compose.%s must be a mapping", r.Name, key)
		}
		if _, _, err := doc.MergeMapping([]string{key}, value, force); err != nil {
			return plannedWrite{}, fmt.Errorf("%s: %w", ComposeOverrideFile, err)
		}
	}

	return finishYAMLPlan(ComposeOverrideFile, doc, before, exists)
}

// planRecipeConfig merges the recipe's config into .orobox.yaml.
//
// `orobox extend` runs without the global config validation, so the file is checked here: editing
// a config that does not load would only bury the real problem under the recipe's keys.
func planRecipeConfig(projectDir string, r Recipe) (plannedWrite, error) {
	src, exists, err := readOptional(filepath.Join(projectDir, configFile))
	if err != nil {
		return plannedWrite{}, err
	}
	if !exists {
		return plannedWrite{}, fmt.Errorf("the %s recipe changes %s and there is none in %s: run `orobox init` first",
			r.Name, configFile, projectDir)
	}
	cfg, err := config.ParseConfig(src)
	if err != nil {
		return plannedWrite{}, fmt.Errorf("%s is not valid, fix it before adding a recipe: %w", configFile, err)
	}
	ini, err := cfg.PhpIniSettings()
	if err != nil {
		return plannedWrite{}, fmt.Errorf("%s: %w", configFile, err)
	}
	if recipeIni := mappingValue(r.Config, "php_ini"); recipeIni != nil && ini.File != "" {
		// The ini file is the project's own, in whatever layout it chose: editing it would be a
		// guess, so the user gets the exact lines instead.
		var lines []string
		for i := 0; i+1 < len(recipeIni.Content); i += 2 {
			lines = append(lines, "  "+recipeIni.Content[i].Value+" = "+recipeIni.Content[i+1].Value)
		}
		return plannedWrite{}, fmt.Errorf("php_ini in %s is the file %s, which orobox does not edit; nothing was changed. Add these settings to it by hand:\n%s",
			configFile, ini.File, strings.Join(lines, "\n"))
	}

	doc, err := yamledit.Parse(src)
	if err != nil {
		return plannedWrite{}, fmt.Errorf("%s: %w", configFile, err)
	}
	before, err := doc.Bytes()
	if err != nil {
		return plannedWrite{}, err
	}
	if err := mergeRecipeConfig(doc, nil, r.Config); err != nil {
		return plannedWrite{}, fmt.Errorf("%s: %w", configFile, err)
	}

	w, err := finishYAMLPlan(configFile, doc, before, exists)
	if err != nil {
		return plannedWrite{}, err
	}
	if w.content != nil {
		// A recipe whose config does not load is a bug in the recipe, and the project must not
		// pay for it with a config every other command refuses.
		if _, err := config.ParseConfig(w.content); err != nil {
			return plannedWrite{}, fmt.Errorf("the %s recipe would make %s invalid: %w", r.Name, configFile, err)
		}
	}
	return w, nil
}

// mergeRecipeConfig walks the recipe's config mapping and applies it under path: mappings are
// descended into, sequences appended without duplicates, scalars added only when absent.
func mergeRecipeConfig(doc *yamledit.Doc, parent []string, fragment *yamlv3.Node) error {
	for i := 0; i+1 < len(fragment.Content); i += 2 {
		key, value := fragment.Content[i], fragment.Content[i+1]
		at := append(append([]string{}, parent...), key.Value)
		switch value.Kind {
		case yamlv3.MappingNode:
			if err := mergeRecipeConfig(doc, at, value); err != nil {
				return err
			}
		case yamlv3.SequenceNode:
			var items []string
			for _, item := range value.Content {
				if item.Kind != yamlv3.ScalarNode {
					return fmt.Errorf("%s: recipe lists may only hold scalars", strings.Join(at, "."))
				}
				items = append(items, item.Value)
			}
			if err := doc.AppendUnique(at, items...); err != nil {
				return err
			}
		default:
			// MergeMapping with a one-key fragment adds the key when absent and leaves an
			// existing value alone, and it keeps the recipe's scalar type (an int stays an int).
			one := &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map", Content: []*yamlv3.Node{key, value}}
			if _, _, err := doc.MergeMapping(parent, one, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// planRecipeEnv appends the recipe's keys the project .env does not define yet. Existing lines are
// kept byte for byte: the project's value always wins.
func planRecipeEnv(projectDir string, r Recipe) (plannedWrite, error) {
	src, exists, err := readOptional(filepath.Join(projectDir, projectEnvFile))
	if err != nil {
		return plannedWrite{}, err
	}
	defined := envKeys(src)

	var missing []string
	for key := range r.Env {
		if !defined[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return plannedWrite{rel: projectEnvFile, action: ActionSkipped}, nil
	}
	sort.Strings(missing)

	var out bytes.Buffer
	out.Write(src)
	if len(src) > 0 {
		if !bytes.HasSuffix(src, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	fmt.Fprintf(&out, "# Added by `orobox extend add %s`\n", r.Name)
	for _, key := range missing {
		out.WriteString(key + "=" + r.Env[key] + "\n")
	}

	action := ActionUpdated
	if !exists {
		action = ActionCreated
	}
	return plannedWrite{rel: projectEnvFile, content: out.Bytes(), action: action}, nil
}

// envKeys returns the keys a dotenv file assigns, read as leniently as the generated-env merge
// reads it: comments, blank lines, "export " and lines without "=" are tolerated.
func envKeys(src []byte) map[string]bool {
	keys := map[string]bool{}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if key = strings.TrimSpace(key); ok && key != "" {
			keys[key] = true
		}
	}
	return keys
}

// planRecipeFiles copies the recipe's files to docker/<name>/, skipping every file that exists.
func planRecipeFiles(projectDir string, r Recipe) ([]plannedWrite, error) {
	var plan []plannedWrite
	err := fs.WalkDir(r.Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := path.Join("docker", r.Name, p)
		switch _, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(rel))); {
		case err == nil:
			plan = append(plan, plannedWrite{rel: rel, action: ActionSkipped})
			return nil
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("could not check %s: %w", rel, err)
		}
		content, err := fs.ReadFile(r.Files, p)
		if err != nil {
			return err
		}
		plan = append(plan, plannedWrite{rel: rel, content: content, action: ActionCreated})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("recipe %s: %w", r.Name, err)
	}
	return plan, nil
}

// finishYAMLPlan turns an edited document into a planned write: created when the file did not
// exist, updated when the edit changed it, skipped otherwise. The comparison is against the
// document as parsed, not the raw source, so formatting alone never counts as a change.
func finishYAMLPlan(rel string, doc *yamledit.Doc, before []byte, exists bool) (plannedWrite, error) {
	after, err := doc.Bytes()
	if err != nil {
		return plannedWrite{}, fmt.Errorf("%s: %w", rel, err)
	}
	switch {
	case !exists:
		return plannedWrite{rel: rel, content: after, action: ActionCreated}, nil
	case !bytes.Equal(before, after):
		return plannedWrite{rel: rel, content: after, action: ActionUpdated}, nil
	}
	return plannedWrite{rel: rel, action: ActionSkipped}, nil
}

// readOptional reads a file that may legitimately be missing.
func readOptional(target string) ([]byte, bool, error) {
	src, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("could not read %s: %w", filepath.Base(target), err)
	}
	return src, true, nil
}

func mappingValue(m *yamlv3.Node, key string) *yamlv3.Node {
	if m == nil || m.Kind != yamlv3.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func hasMappingKey(m *yamlv3.Node, key string) bool {
	v := mappingValue(m, key)
	return v != nil && v.Kind == yamlv3.MappingNode && len(v.Content) > 0
}

func mappingKeys(m *yamlv3.Node) []string {
	if m == nil || m.Kind != yamlv3.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	return keys
}
