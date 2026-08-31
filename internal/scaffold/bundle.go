package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// BundleOptions holds the resolved template variables for a bundle skeleton.
type BundleOptions struct {
	ClassName   string // e.g. AcmeFooBundle
	Namespace   string // e.g. Acme\Bundle\FooBundle
	Prefix      string // ClassName without a trailing "Bundle", e.g. AcmeFoo
	Alias       string // snake_case of Prefix, e.g. acme_foo
	PackageName string // composer package, e.g. acme/foo-bundle

	// Standalone says the bundle owns its own composer package: it carries a composer.json
	// declaring its PSR-4 root and a .gitignore. A bundle generated inside a project's own
	// PSR-4 tree carries neither — the project's composer.json already autoloads it, and a
	// second composer.json there would make composer treat the directory as a nested package.
	Standalone bool
}

// ParseBundleArg derives the bundle template variables from the CLI argument and optional
// overrides.
//
// The argument is the bundle *namespace* — `Acme\Bundle\FooBundle`, the form the PSR-4 map
// resolves to a directory — because that is the one input that decides both where the bundle
// lands and what its class is called. A fully-qualified class
// (`Acme\Bundle\FooBundle\AcmeFooBundle`) is accepted too: the two are told apart by the
// segment before the last one, which in the class form is itself a `*Bundle` namespace segment
// and in the namespace form is either a vendor or Oro's literal `Bundle` separator.
func ParseBundleArg(arg, classOverride, packageOverride string) (BundleOptions, error) {
	arg = strings.Trim(strings.TrimSpace(arg), `\`)
	if arg == "" {
		return BundleOptions{}, errors.New("bundle namespace is required")
	}

	segments := strings.Split(arg, `\`)
	for _, s := range segments {
		if s == "" {
			return BundleOptions{}, fmt.Errorf("bundle namespace %q has an empty segment", arg)
		}
	}

	namespace := arg
	className := ""
	if n := len(segments); n >= 2 && looksLikeBundleNamespace(segments[n-2]) {
		className = segments[n-1]
		namespace = strings.Join(segments[:n-1], `\`)
		segments = segments[:n-1]
	}

	if classOverride != "" {
		className = classOverride
	}
	if className == "" {
		className = deriveClassName(segments)
	}

	prefix := strings.TrimSuffix(className, "Bundle")
	if prefix == "" {
		prefix = className
	}

	packageName := packageOverride
	if packageName == "" {
		packageName = derivePackageName(segments, className)
	}

	return BundleOptions{
		ClassName:   className,
		Namespace:   namespace,
		Prefix:      prefix,
		Alias:       snakeCase(prefix),
		PackageName: packageName,
	}, nil
}

// looksLikeBundleNamespace reports whether a namespace segment is itself a bundle namespace
// (`FooBundle`), which is what marks the segment after it as a class name. Oro's own
// `Acme\Bundle\FooBundle` layout uses a literal `Bundle` separator segment, so that exact
// spelling does not count.
func looksLikeBundleNamespace(segment string) bool {
	return segment != "Bundle" && strings.HasSuffix(segment, "Bundle")
}

// deriveClassName builds the Oro-conventional class name from the namespace: the vendor
// segment prepended to the bundle segment, so `Acme\Bundle\FooBundle` yields `AcmeFooBundle`
// exactly as `Oro\Bundle\UserBundle` yields `OroUserBundle`. A bundle segment that already
// starts with the vendor is left alone rather than doubled.
func deriveClassName(segments []string) string {
	last := segments[len(segments)-1]
	if len(segments) < 2 {
		return last
	}
	vendor := segments[0]
	if strings.HasPrefix(last, vendor) {
		return last
	}
	return vendor + last
}

// derivePackageName builds a composer "vendor/package" name. When the namespace has multiple
// segments, the first becomes the vendor and the last the package; otherwise it falls back to
// "orobox/<kebab-class>".
func derivePackageName(segments []string, className string) string {
	if len(segments) >= 2 {
		return kebabCase(segments[0]) + "/" + kebabCase(segments[len(segments)-1])
	}
	return "orobox/" + kebabCase(className)
}

// snakeCase converts a PascalCase identifier to snake_case ("AcmeFoo" -> "acme_foo").
func snakeCase(s string) string {
	return splitCamel(s, "_")
}

// kebabCase converts a PascalCase identifier to kebab-case ("AcmeFoo" -> "acme-foo").
func kebabCase(s string) string {
	return splitCamel(s, "-")
}

func splitCamel(s, sep string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// BundleArtifacts is the file set of a bundle skeleton, relative to the bundle's own
// directory. Every file is WriteOnce: a bundle is scaffolded once and owned by its author from
// then on, unlike the QA and CI stubs this package also writes.
//
// The composer.json and .gitignore appear only for a standalone bundle — see
// BundleOptions.Standalone.
func BundleArtifacts(opts BundleOptions) []Artifact {
	artifacts := []Artifact{
		{RelPath: opts.ClassName + ".php", TemplatePath: "templates/bundle/bundle.php.tmpl", Ownership: WriteOnce},
		{
			RelPath:      filepath.Join("DependencyInjection", opts.Prefix+"Extension.php"),
			TemplatePath: "templates/bundle/Extension.php.tmpl",
			Ownership:    WriteOnce,
		},
		{
			RelPath:      filepath.Join("DependencyInjection", "Configuration.php"),
			TemplatePath: "templates/bundle/Configuration.php.tmpl",
			Ownership:    WriteOnce,
		},
		{
			RelPath:      filepath.Join("Resources", "config", "services.yml"),
			TemplatePath: "templates/bundle/services.yml.tmpl",
			Ownership:    WriteOnce,
		},
		{
			RelPath:      filepath.Join("Resources", "config", "oro", "bundles.yml"),
			TemplatePath: "templates/bundle/bundles.yml.tmpl",
			Ownership:    WriteOnce,
		},
	}
	if opts.Standalone {
		artifacts = append(artifacts,
			Artifact{RelPath: "composer.json", TemplatePath: "templates/bundle/composer.json.tmpl", Ownership: WriteOnce},
			Artifact{RelPath: ".gitignore", TemplatePath: "templates/bundle/gitignore.tmpl", Ownership: WriteOnce},
		)
	}
	return artifacts
}

// Bundle renders the bundle skeleton into destDir. It refuses a directory that already exists
// and is non-empty: a half-generated bundle mixed into someone else's files is worse than a
// command that did nothing.
func Bundle(destDir string, opts BundleOptions) error {
	if err := ensureEmptyDir(destDir); err != nil {
		return err
	}
	if _, err := WriteAll(destDir, BundleArtifacts(opts), opts); err != nil {
		return err
	}
	return nil
}

// ensureEmptyDir returns an error if destDir exists and is non-empty; otherwise it creates it.
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
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating target directory %q: %w", destDir, err)
	}
	return nil
}

// BundlePlacement is the decision of where a bundle skeleton goes and what shape it takes.
type BundlePlacement struct {
	// Dir is the bundle's directory, relative to projectRoot.
	Dir string
	// Standalone mirrors BundleOptions.Standalone: true when the bundle carries its own
	// composer package rather than living inside the project's PSR-4 tree.
	Standalone bool
	// Psr4 is the matched PSR-4 entry, populated only when Standalone is false.
	Psr4 Psr4Target
}

// ResolveBundlePlacement decides where a bundle namespace lands under projectRoot.
//
// Inside an OroCommerce checkout the answer comes from the project's own composer.json: its
// `"": "src/"` rule puts `Acme\Bundle\FooBundle` in `src/Acme/Bundle/FooBundle`, autoloaded by
// the project and discovered by Oro's kernel through the generated
// `Resources/config/oro/bundles.yml`. Outside one — or with forceStandalone — the bundle
// becomes its own composer package in a directory named after its class.
//
// pathOverride wins over both and is taken relative to projectRoot (an absolute path is used
// as given); it changes the location, not the shape.
func ResolveBundlePlacement(projectRoot string, opts BundleOptions, pathOverride string, forceStandalone bool) (BundlePlacement, error) {
	placement := BundlePlacement{Standalone: true, Dir: opts.ClassName}

	if !forceStandalone {
		target, err := ResolvePsr4Dir(projectRoot, opts.Namespace)
		switch {
		case err == nil:
			placement = BundlePlacement{Dir: target.RelPath, Standalone: false, Psr4: target}
		case errors.Is(err, ErrNoPsr4Root):
			// Not a PHP project, or its PSR-4 map does not reach this namespace: the bundle
			// has to autoload itself, which is what a standalone package is for.
		default:
			return BundlePlacement{}, err
		}
	}

	if pathOverride != "" {
		placement.Dir = pathOverride
	}
	return placement, nil
}

// Dest is the placement's directory as a path the filesystem can use: joined onto projectRoot
// unless it is already absolute.
func (p BundlePlacement) Dest(projectRoot string) string {
	if filepath.IsAbs(p.Dir) {
		return p.Dir
	}
	return filepath.Join(projectRoot, p.Dir)
}
