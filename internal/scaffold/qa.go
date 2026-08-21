package scaffold

import "github.com/algoritma-dev/orobox/internal/config"

// QaStubData is what the QA stub templates render against.
//
// The fields are PHP fragments rather than paths because the stubs are the files that decide which
// tree PHP-CS-Fixer and Twig-CS-Fixer walk: both merges take the rules from either side but the
// finder from the project's file, so a stub without one is a regression rather than a neutral
// starting point. The tree differs per install type — a project analyses its own src/, a bundle its
// whole checkout — which is the whole reason these are rendered and not copied.
type QaStubData struct {
	// PhpFinderPath is the PHP expression naming the tree PHP-CS-Fixer walks.
	PhpFinderPath string
	// PhpFinderExclude is the chained ->exclude(...) call, empty when nothing is excluded.
	PhpFinderExclude string
	// TwigPaths is the PHP array literal of directories Twig-CS-Fixer walks.
	TwigPaths string
	// TwigExclude is the $finder->exclude(...) statement, empty when nothing is excluded.
	TwigExclude string
}

// QaStubDataFor builds the stub data for an install type.
//
// __DIR__ is correct for both without a second path mapping: the stub sits at the host source
// root, which is bound at OroRoot for a project and at OroRoot/bundles/<Namespace> for a bundle.
func QaStubDataFor(typeName string) QaStubData {
	if typeName == config.InstallTypeBundle || typeName == "" {
		return QaStubData{
			PhpFinderPath:    "__DIR__",
			PhpFinderExclude: "\n    ->exclude(['vendor', 'node_modules'])",
			TwigPaths:        "[__DIR__]",
			TwigExclude:      "$finder->exclude(['vendor', 'node_modules']);\n",
		}
	}

	return QaStubData{
		PhpFinderPath: "__DIR__ . '/src'",
		TwigPaths:     "[__DIR__ . '/templates', __DIR__ . '/src']",
	}
}

// QaStubs returns the QA configuration stubs to scaffold for an install type, gated on the tools
// the configuration enables: a project that turned Rector off never gets a rector.php to explain
// away.
//
// The JS stubs are bundle-only, and that is correctness rather than preference. qatools.mergedConfig
// resolves the ESLint and Stylelint base configs against OroRoot, which for a project install *is*
// the source root, and it skips merging when the two paths are equal — so a stub written there
// would overwrite OroCommerce's own configuration instead of layering on it.
//
// The ignore files (.eslintignore, .stylelintignore, .stylelintignore-css) are deliberately absent
// from every install type. They are not merged but chosen, project-file-or-base, because their
// patterns are gitignore-style and re-anchor when copied elsewhere; a stub would replace Oro's
// ignore list wholesale.
func QaStubs(typeName string) []Artifact {
	var artifacts []Artifact

	add := func(relPath, templatePath string) {
		artifacts = append(artifacts, Artifact{
			RelPath:      relPath,
			TemplatePath: templatePath,
			Ownership:    WriteOnce,
		})
	}

	if config.IsQaToolEnabled("phpstan") {
		add("phpstan.neon", "templates/qa/phpstan.neon.tmpl")
	}
	if config.IsQaToolEnabled("rector") {
		add("rector.php", "templates/qa/rector.php.tmpl")
	}
	if config.IsQaToolEnabled("php-cs-fixer") {
		add(".php-cs-fixer.dist.php", "templates/qa/php-cs-fixer.dist.php.tmpl")
	}
	if config.IsQaToolEnabled("twig-cs-fixer") {
		add(".twig-cs-fixer.php", "templates/qa/twig-cs-fixer.php.tmpl")
	}

	if typeName == config.InstallTypeBundle || typeName == "" {
		if config.IsQaToolEnabled("eslint") {
			add(".eslintrc.yml", "templates/qa/eslintrc.yml.tmpl")
		}
		if config.IsQaToolEnabled("stylelint") {
			add(".stylelintrc.yml", "templates/qa/stylelintrc.yml.tmpl")
			add(".stylelintrc-css.yml", "templates/qa/stylelintrc-css.yml.tmpl")
		}
	}

	return artifacts
}
