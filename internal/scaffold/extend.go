package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/yamledit"

	yamlv3 "gopkg.in/yaml.v3"
)

// Receipt actions. Every `orobox extend` subcommand reports what it did to each file as one
// Receipt, so a caller (a person, or an agent reading `--agent` output) can see exactly which
// files were touched without diffing the tree.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
	ActionSkipped = "skipped"
)

// Receipt is one line of an `orobox extend` report: what happened to Path, which is relative to
// the project directory and always uses forward slashes so the output is the same on every OS.
type Receipt struct{ Path, Action string }

// String renders the receipt as the single line `orobox extend` prints.
func (r Receipt) String() string { return r.Action + " " + r.Path }

const (
	// defaultDockerfile is where `extend image` puts the Dockerfile when the config does not name
	// one already. It gets a directory of its own because that directory is the build context:
	// every file in it is hashed on each container start and a change triggers a rebuild, so it
	// must not share docker/ with recipe data, fixtures or uploads that change at run time.
	defaultDockerfile = "docker/image/Dockerfile"

	// ComposeOverrideFile is the shared override file Orobox discovers by name. It and
	// ComposeLocalOverrideFile repeat docker.OverrideFiles on purpose — scaffold writes files and
	// must not depend on how the docker package finds them — and a test keeps the two in step.
	ComposeOverrideFile = ".orobox.compose.yaml"
	// ComposeLocalOverrideFile is the uncommitted, per-developer override file Orobox discovers by
	// name.
	ComposeLocalOverrideFile = ".orobox.compose.local.yaml"

	// gitignoreEntry is what `extend compose --local` adds to .gitignore. Anchored to the project
	// root because that is where the file lives.
	gitignoreEntry = "/" + ComposeLocalOverrideFile
)

// project is the config file an `orobox extend` subcommand works against and the directory
// holding it, where every other file it writes lives. The config is the one in use — the
// --config file when one was given — so the command edits the file every other command reads.
type project struct {
	dir    string // directory holding the config file
	config string // the config file's name, as receipts report it
	src    []byte // the config file's content
}

// openProject reads the config file at configPath. Every extend subcommand requires it: without
// a config there is no Orobox project, and the files would land where Orobox never looks.
func openProject(configPath string) (project, error) {
	p := project{dir: filepath.Dir(configPath), config: filepath.Base(configPath)}
	src, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return p, fmt.Errorf("no %s in %s: run `orobox init` first", p.config, p.dir)
	}
	if err != nil {
		return p, fmt.Errorf("could not read %s: %w", p.config, err)
	}
	p.src = src
	return p, nil
}

// ExtendImage writes the project Dockerfile and points image.dockerfile at it.
//
// When the config already names a Dockerfile (under image.dockerfile or the deprecated top-level
// key) that path is used and the config is left untouched, so running the command never leaves a
// second Dockerfile behind or rewrites a choice the user made. An existing Dockerfile is never
// overwritten; one at the default path that the config does not name is adopted only when it is
// an Orobox layer (final stage FROM ${OROBOX_BASE_IMAGE}).
func ExtendImage(configPath string) ([]Receipt, error) {
	p, err := openProject(configPath)
	if err != nil {
		return nil, err
	}

	doc, err := yamledit.Parse(p.src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.config, err)
	}
	configured, err := configuredDockerfile(p.config, p.src)
	if err != nil {
		return nil, err
	}

	dockerfile := configured
	var updatedConfig []byte
	if configured == "" {
		dockerfile = defaultDockerfile
		if err := checkAdoptable(p.dir, dockerfile); err != nil {
			return nil, err
		}
		// The config edit is the step that can still fail, so it is prepared before the first
		// file is written: a failure must not leave a Dockerfile nothing references.
		if err := doc.SetScalar([]string{"image", "dockerfile"}, dockerfile); err != nil {
			return nil, fmt.Errorf("%s: %w", p.config, err)
		}
		if updatedConfig, err = doc.Bytes(); err != nil {
			return nil, fmt.Errorf("%s: %w", p.config, err)
		}
		// Same reason: a config the write would refuse (a symlink out of the project) is found
		// before the Dockerfile exists.
		if err := checkWritable(p.dir, p.config); err != nil {
			return nil, err
		}
	}

	var receipts []Receipt
	receipt, err := writeTemplateOnce(p.dir, dockerfile, "templates/extend/Dockerfile.tmpl")
	if err != nil {
		return receipts, err
	}
	receipts = append(receipts, receipt)

	if updatedConfig == nil {
		return append(receipts, Receipt{Path: p.config, Action: ActionSkipped}), nil
	}
	if err := writeInsideProject(p.dir, p.config, updatedConfig, false); err != nil {
		return receipts, err
	}
	return append(receipts, Receipt{Path: p.config, Action: ActionUpdated}), nil
}

// checkAdoptable refuses to point the config at an existing file that is not an Orobox layer
// Dockerfile: a project's own production Dockerfile at that path would otherwise be built as the
// development layer, or rejected by the next `orobox up` far from this command.
func checkAdoptable(dir, rel string) error {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not check %s: %w", rel, err)
	}
	// Only a regular file can be the layer Dockerfile: a directory, or a symlink (dangling, or
	// pointing who knows where), would be "skipped" by the write and then built by `orobox up`.
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s already exists and is not a regular file; set image.dockerfile to another path by hand", rel)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", rel, err)
	}
	if err := docker.CheckExtendsBaseImage(rel, content); err != nil {
		return fmt.Errorf("%s already exists and is not an Orobox layer Dockerfile (%v); set image.dockerfile to another path by hand", rel, err)
	}
	return nil
}

// ExtendCompose writes the compose override, or with local the git-ignored personal one, and
// registers the personal one in .gitignore.
//
// The .gitignore is only ever appended to, and only when it already exists: creating one in a
// directory that does not track files this way would be a surprise, and a bundle or project
// without one has nothing to leak the local file into.
func ExtendCompose(configPath string, local bool) ([]Receipt, error) {
	p, err := openProject(configPath)
	if err != nil {
		return nil, err
	}
	projectDir := p.dir

	name, tmpl := ComposeOverrideFile, "templates/extend/compose.yaml.tmpl"
	if local {
		name, tmpl = ComposeLocalOverrideFile, "templates/extend/compose.local.yaml.tmpl"
	}

	// The .gitignore write is checked first: a local override created without its ignore entry
	// is the one file this command must not leave behind for git to pick up.
	if local {
		if err := checkWritable(projectDir, ".gitignore"); err != nil {
			return nil, err
		}
	}
	receipt, err := writeTemplateOnce(projectDir, name, tmpl)
	if err != nil {
		return nil, err
	}
	receipts := []Receipt{receipt}

	if !local {
		return receipts, nil
	}
	gitignore, err := ignoreLocalOverride(projectDir)
	if err != nil {
		return receipts, err
	}
	if gitignore != nil {
		receipts = append(receipts, *gitignore)
	}
	return receipts, nil
}

// configuredDockerfile returns the project-relative Dockerfile the config already names, or ""
// when it names none. image.dockerfile wins over the deprecated top-level key, as in
// config.ImageSettings.
//
// The config is decoded loosely rather than through config.ParseConfig: this command only needs
// two keys, and rejecting a file over an unrelated field (one a newer Orobox added, say) would
// make an unrelated problem block it.
func configuredDockerfile(configFile string, src []byte) (string, error) {
	var cfg struct {
		Dockerfile string `yaml:"dockerfile"`
		Image      *struct {
			Dockerfile string `yaml:"dockerfile"`
		} `yaml:"image"`
	}
	if err := yamlv3.Unmarshal(src, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", configFile, err)
	}

	raw := cfg.Dockerfile
	if cfg.Image != nil && strings.TrimSpace(cfg.Image.Dockerfile) != "" {
		raw = cfg.Image.Dockerfile
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	cleaned := strings.TrimPrefix(path.Clean(filepath.ToSlash(raw)), "./")
	if filepath.IsAbs(raw) || path.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%s: the Dockerfile %q must be a path inside the project", configFile, raw)
	}
	return cleaned, nil
}

// writeTemplateOnce renders tmpl into rel under projectDir unless a file is already there, and
// says which of the two happened. The write is exclusive and confined to the project, so a file
// that appeared since the check is never overwritten and a symlink never leads it outside.
func writeTemplateOnce(projectDir, rel, tmpl string) (Receipt, error) {
	receipt := Receipt{Path: filepath.ToSlash(rel), Action: ActionSkipped}
	// Lstat: a dangling symlink is an existing entry, never something to write through.
	switch _, err := os.Lstat(filepath.Join(projectDir, filepath.FromSlash(rel))); {
	case err == nil:
		return receipt, nil
	case !errors.Is(err, os.ErrNotExist):
		return Receipt{}, fmt.Errorf("could not check %s: %w", rel, err)
	}
	rendered, err := Render(tmpl, nil)
	if err != nil {
		return Receipt{}, fmt.Errorf("could not render %s: %w", rel, err)
	}
	if err := writeInsideProject(projectDir, filepath.ToSlash(rel), rendered, true); err != nil {
		return Receipt{}, err
	}
	receipt.Action = ActionCreated
	return receipt, nil
}

// ignoreLocalOverride appends the personal override to .gitignore. It returns nil when there is no
// .gitignore to extend.
func ignoreLocalOverride(projectDir string) (*Receipt, error) {
	target := filepath.Join(projectDir, ".gitignore")
	existing, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read .gitignore: %w", err)
	}

	for _, line := range strings.Split(string(existing), "\n") {
		// With or without the slash anchor means the same file here, and either spelling in an
		// existing .gitignore is the user's own, so it counts.
		if entry := strings.TrimSpace(line); entry == gitignoreEntry || entry == strings.TrimPrefix(gitignoreEntry, "/") {
			return &Receipt{Path: ".gitignore", Action: ActionSkipped}, nil
		}
	}

	updated := string(existing)
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	updated += gitignoreEntry + "\n"
	if err := writeInsideProject(projectDir, ".gitignore", []byte(updated), false); err != nil {
		return nil, err
	}
	return &Receipt{Path: ".gitignore", Action: ActionUpdated}, nil
}
