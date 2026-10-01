package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	// configFile is the project config `extend image` edits.
	configFile = ".orobox.yaml"
	// defaultDockerfile is where `extend image` puts the Dockerfile when the config does not name
	// one already.
	defaultDockerfile = "docker/Dockerfile"

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

// ExtendImage writes the project Dockerfile and points image.dockerfile at it.
//
// The project must already have a .orobox.yaml: the Dockerfile only means something once the
// config references it, and creating a config here would skip everything `orobox init` decides.
// When the config already names a Dockerfile (under image.dockerfile or the deprecated top-level
// key) that path is used and the config is left untouched, so running the command never leaves a
// second Dockerfile behind or rewrites a choice the user made. An existing Dockerfile is never
// overwritten.
func ExtendImage(projectDir string) ([]Receipt, error) {
	configPath := filepath.Join(projectDir, configFile)
	src, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no %s in %s: run `orobox init` first", configFile, projectDir)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", configFile, err)
	}

	// Everything that can fail on the config is checked before the first file is written, so a
	// broken config does not leave a Dockerfile behind that nothing references.
	doc, err := yamledit.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", configFile, err)
	}
	configured, err := configuredDockerfile(src)
	if err != nil {
		return nil, err
	}

	dockerfile := configured
	if dockerfile == "" {
		dockerfile = defaultDockerfile
	}

	var receipts []Receipt
	receipt, err := writeTemplateOnce(projectDir, dockerfile, "templates/extend/Dockerfile.tmpl")
	if err != nil {
		return receipts, err
	}
	receipts = append(receipts, receipt)

	if configured != "" {
		return append(receipts, Receipt{Path: configFile, Action: ActionSkipped}), nil
	}

	if err := doc.SetScalar([]string{"image", "dockerfile"}, dockerfile); err != nil {
		return receipts, fmt.Errorf("%s: %w", configFile, err)
	}
	out, err := doc.Bytes()
	if err != nil {
		return receipts, fmt.Errorf("%s: %w", configFile, err)
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return receipts, fmt.Errorf("could not write %s: %w", configFile, err)
	}
	return append(receipts, Receipt{Path: configFile, Action: ActionUpdated}), nil
}

// ExtendCompose writes the compose override, or with local the git-ignored personal one, and
// registers the personal one in .gitignore.
//
// The .gitignore is only ever appended to, and only when it already exists: creating one in a
// directory that does not track files this way would be a surprise, and a bundle or project
// without one has nothing to leak the local file into.
func ExtendCompose(projectDir string, local bool) ([]Receipt, error) {
	name, tmpl := ComposeOverrideFile, "templates/extend/compose.yaml.tmpl"
	if local {
		name, tmpl = ComposeLocalOverrideFile, "templates/extend/compose.local.yaml.tmpl"
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
func configuredDockerfile(src []byte) (string, error) {
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
// says which of the two happened. It goes through Write so the never-overwrite rule is the one
// every other generated file already follows.
func writeTemplateOnce(projectDir, rel, tmpl string) (Receipt, error) {
	res, err := Write(projectDir, Artifact{RelPath: rel, TemplatePath: tmpl, Ownership: WriteOnce}, nil)
	if err != nil {
		return Receipt{}, err
	}
	action := ActionCreated
	if res.Skipped {
		action = ActionSkipped
	}
	return Receipt{Path: filepath.ToSlash(rel), Action: action}, nil
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
	if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("could not write .gitignore: %w", err)
	}
	return &Receipt{Path: ".gitignore", Action: ActionUpdated}, nil
}
