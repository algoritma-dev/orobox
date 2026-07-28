package scaffold

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// templateFuncs are available to all scaffold templates. "esc" doubles backslashes so a
// PHP namespace can be embedded safely inside a JSON string (composer.json PSR-4 keys).
var templateFuncs = template.FuncMap{
	"esc": func(s string) string { return strings.ReplaceAll(s, `\`, `\\`) },
}

// Templates holds the embedded filesystem for scaffold templates. It is wired from
// main.go (the same embed that backs docker.Templates) and replaced with an in-memory
// FS in tests.
var Templates fs.FS

// bundleFiles maps each embedded template to the output path (relative to the bundle
// root) it renders to. The output path is a function of the resolved options.
var bundleFiles = []struct {
	tmpl string
	out  func(BundleOptions) string
}{
	{"templates/bundle/bundle.php.tmpl", func(o BundleOptions) string { return o.ClassName + ".php" }},
	{"templates/bundle/Extension.php.tmpl", func(o BundleOptions) string {
		return filepath.Join("DependencyInjection", o.Prefix+"Extension.php")
	}},
	{"templates/bundle/Configuration.php.tmpl", func(BundleOptions) string {
		return filepath.Join("DependencyInjection", "Configuration.php")
	}},
	{"templates/bundle/services.yml.tmpl", func(BundleOptions) string {
		return filepath.Join("Resources", "config", "services.yml")
	}},
	{"templates/bundle/bundles.yml.tmpl", func(BundleOptions) string {
		return filepath.Join("Resources", "config", "oro", "bundles.yml")
	}},
	{"templates/bundle/composer.json.tmpl", func(BundleOptions) string { return "composer.json" }},
	{"templates/bundle/gitignore.tmpl", func(BundleOptions) string { return ".gitignore" }},
}

// Bundle renders the embedded bundle templates into destDir. It refuses to
// write into a directory that already exists and is non-empty.
func Bundle(destDir string, opts BundleOptions) error {
	if err := ensureEmptyDir(destDir); err != nil {
		return err
	}

	for _, f := range bundleFiles {
		rendered, err := renderTemplate(f.tmpl, opts)
		if err != nil {
			return err
		}
		outPath := filepath.Join(destDir, f.out(opts))
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(outPath), err)
		}
		if err := os.WriteFile(outPath, rendered, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
	}
	return nil
}

func renderTemplate(name string, opts BundleOptions) ([]byte, error) {
	data, err := fs.ReadFile(Templates, name)
	if err != nil {
		return nil, fmt.Errorf("reading template %s: %w", name, err)
	}
	tmpl, err := template.New(filepath.Base(name)).Funcs(templateFuncs).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts); err != nil {
		return nil, fmt.Errorf("rendering template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// ensureEmptyDir returns an error if destDir exists and is non-empty; otherwise it
// creates it.
func ensureEmptyDir(destDir string) error {
	entries, err := os.ReadDir(destDir)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("target directory %q already exists and is not empty", destDir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("checking target directory %q: %w", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating target directory %q: %w", destDir, err)
	}
	return nil
}
