// Package scaffold renders the files Orobox generates into a project's own checkout.
//
// It is the one place that answers two questions for every such file: where it lives relative to
// the repository root, and who owns it once it is there. Both answers already existed, restated in
// three places — the deploy pair in cmd/deploy_init.go, the Twig config in
// qatools.TwigConfigScript, the env seeds in cmd/init.go — and this package is where they are
// stated once.
package scaffold

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Templates holds the embedded filesystem for scaffold templates.
// It is assigned in main, next to docker.Templates and pipeline.Templates.
var Templates fs.FS

// The rendered files are PHP and GitLab YAML, which use {} and $ for their own interpolation, so
// the Go templates use [[ ]] — the same choice internal/pipeline/templates.go made.
const (
	leftDelim  = "[["
	rightDelim = "]]"
)

// templateFuncs are available to every scaffold template. "esc" doubles backslashes so a PHP
// namespace can be embedded inside a JSON string — composer.json's PSR-4 keys are the case
// that needs it. "json" is the general form: it escapes any value for a JSON string body, so
// a CLI override cannot break the structure of a generated composer.json.
var templateFuncs = template.FuncMap{
	"esc":  func(s string) string { return strings.ReplaceAll(s, `\`, `\\`) },
	"json": jsonString,
}

// jsonString encodes s as a JSON string and strips the surrounding quotes, so the result can
// be dropped inside a quoted string already present in a template.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return string(b[1 : len(b)-1])
}

// Ownership says what an already existing target file means.
type Ownership int

const (
	// WriteOnce leaves an existing file untouched: the project owns it from the first write on.
	WriteOnce Ownership = iota
	// Rewrite overwrites on every run, so improvements to a file Orobox owns reach existing
	// projects without a manual merge.
	Rewrite
)

// Artifact is one file Orobox generates into a repository.
type Artifact struct {
	// RelPath is where the file lands, relative to the repository root.
	RelPath string
	// TemplatePath is the path of its template inside Templates.
	TemplatePath string
	// Ownership decides what an existing file at RelPath means.
	Ownership Ownership
}

// Result reports what Write did, so a command can tell the user whether a file was created,
// refreshed, or deliberately left alone.
type Result struct {
	Artifact Artifact
	Written  bool
	Skipped  bool
}

// Write renders one artifact into root.
//
// A WriteOnce artifact whose file exists is skipped rather than reported as an error: re-running an
// init command is the normal way to pick up a newly enabled tool, and the existing file is the
// project's.
func Write(root string, a Artifact, data any) (Result, error) {
	target := filepath.Join(root, a.RelPath)

	if a.Ownership == WriteOnce {
		// Lstat, not Stat: a dangling symlink is an existing entry, and following it would
		// create the file wherever the link points.
		switch _, err := os.Lstat(target); {
		case err == nil:
			return Result{Artifact: a, Skipped: true}, nil
		case !errors.Is(err, os.ErrNotExist):
			return Result{Artifact: a}, fmt.Errorf("could not check %s: %w", a.RelPath, err)
		}
	}

	rendered, err := Render(a.TemplatePath, data)
	if err != nil {
		return Result{Artifact: a}, fmt.Errorf("could not render %s: %w", a.RelPath, err)
	}

	// Generators may write into a directory a project deliberately links elsewhere (a shared
	// .gitlab, say), so Write does not confine the target; `orobox extend` uses
	// writeInsideProject, which does.
	if err := writeFile(root, filepath.ToSlash(a.RelPath), rendered, a.Ownership == WriteOnce, false); err != nil {
		return Result{Artifact: a}, err
	}
	return Result{Artifact: a, Written: true}, nil
}

// writeInsideProject writes content to rel under root, creating the missing directories, and
// refuses whatever would land outside root: a directory that resolves elsewhere through a
// symlink, or a target that is itself a symlink pointing out of the project. With exclusive it
// creates the file only if nothing exists there, so a file that appeared since it was checked is
// never overwritten. `orobox extend` writes every file through it.
func writeInsideProject(root, rel string, content []byte, exclusive bool) error {
	if err := checkWritable(root, rel); err != nil {
		return err
	}
	return writeFile(root, rel, content, exclusive, true)
}

// checkWritable reports whether rel can be written under root without leaving it. It creates
// nothing, so a caller planning several writes can check them all before the first one.
func checkWritable(root, rel string) error {
	target := filepath.Join(root, filepath.FromSlash(rel))
	// The nearest existing ancestor is checked, not the directory itself: MkdirAll would
	// otherwise build the missing directories wherever a symlinked parent points.
	if err := checkInside(root, existingAncestor(filepath.Dir(target))); err != nil {
		return fmt.Errorf("refusing to write %s: %w", rel, err)
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("refusing to write %s: it is a symlink that leads nowhere", rel)
	}
	if err := checkInside(root, filepath.Dir(resolved)); err != nil {
		return fmt.Errorf("refusing to write %s: it is a symlink to %s, outside the project", rel, resolved)
	}
	return nil
}

// writeFile writes content to rel under root. confine re-checks, once the directories exist,
// that the target's directory is still inside root.
func writeFile(root, rel string, content []byte, exclusive, confine bool) error {
	target := filepath.Join(root, filepath.FromSlash(rel))
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	if confine {
		if err := checkInside(root, dir); err != nil {
			return fmt.Errorf("refusing to write %s: %w", rel, err)
		}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(target, flags, 0o644)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", rel, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("could not write %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", rel, err)
	}
	return nil
}

// existingAncestor returns dir, or its closest ancestor that exists.
func existingAncestor(dir string) string {
	for {
		if _, err := os.Lstat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// checkInside reports an error when dir, with symlinks resolved, is not root or below it.
func checkInside(root, dir string) error {
	// Both paths are made absolute first: EvalSymlinks keeps a relative path relative, and Rel
	// between a relative root and an absolute target (or the reverse) cannot be computed.
	realRoot, err := resolvePath(root)
	if err != nil {
		return err
	}
	realDir, err := resolvePath(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves to %s, outside the project", dir, realDir)
	}
	return nil
}

// resolvePath returns path made absolute, with every symlink in it resolved.
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// WriteAll writes every artifact against the same data and stops at the first failure: a
// half-written set is easier to reason about than one where a later file silently contradicts an
// earlier one. The results of the writes that did happen are returned alongside the error.
func WriteAll(root string, artifacts []Artifact, data any) ([]Result, error) {
	results := make([]Result, 0, len(artifacts))
	for _, artifact := range artifacts {
		result, err := Write(root, artifact, data)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// Render renders one template from Templates against data.
func Render(path string, data any) ([]byte, error) {
	raw, err := fs.ReadFile(Templates, path)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New(filepath.Base(path)).Delims(leftDelim, rightDelim).Funcs(templateFuncs).Parse(string(raw))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
