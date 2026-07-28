// Package scaffold creates local source trees for Orobox: a generated bundle
// skeleton or a cloned OroCommerce project checkout. It performs no Docker or
// configuration work — that stays with `orobox init`.
package scaffold

import (
	"errors"
	"strings"
	"unicode"
)

// BundleOptions holds the resolved template variables for a bundle skeleton.
type BundleOptions struct {
	ClassName   string // e.g. AcmeFooBundle
	Namespace   string // e.g. Acme\FooBundle
	Prefix      string // ClassName without a trailing "Bundle", e.g. AcmeFoo
	Alias       string // snake_case of Prefix, e.g. acme_foo
	PackageName string // composer package, e.g. acme/foo-bundle
}

// ParseBundleArg derives the bundle template variables from the CLI argument and
// optional overrides. The argument may be a short class name ("AcmeFooBundle") or a
// fully-qualified class ("Acme\FooBundle\AcmeFooBundle").
func ParseBundleArg(arg, namespaceOverride, packageOverride string) (BundleOptions, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return BundleOptions{}, errors.New("bundle class name is required")
	}

	var className, namespace string
	if idx := strings.LastIndex(arg, `\`); idx != -1 {
		namespace = arg[:idx]
		className = arg[idx+1:]
	} else {
		className = arg
		if namespaceOverride != "" {
			namespace = namespaceOverride
		} else {
			namespace = className
		}
	}

	if namespaceOverride != "" {
		namespace = namespaceOverride
	}

	if className == "" {
		return BundleOptions{}, errors.New("bundle class name is empty")
	}

	prefix := strings.TrimSuffix(className, "Bundle")
	if prefix == "" {
		prefix = className
	}

	packageName := packageOverride
	if packageName == "" {
		packageName = derivePackageName(namespace, className)
	}

	return BundleOptions{
		ClassName:   className,
		Namespace:   namespace,
		Prefix:      prefix,
		Alias:       snakeCase(prefix),
		PackageName: packageName,
	}, nil
}

// derivePackageName builds a composer "vendor/package" name. When the namespace has
// multiple segments, the first becomes the vendor and the last the package; otherwise
// it falls back to "orobox/<kebab-class>".
func derivePackageName(namespace, className string) string {
	segments := strings.Split(namespace, `\`)
	if len(segments) >= 2 {
		vendor := kebabCase(segments[0])
		pkg := kebabCase(segments[len(segments)-1])
		return vendor + "/" + pkg
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
