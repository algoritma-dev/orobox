package qatools

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
)

// mergedDir is where the merged configs are written. It lives inside the isolated QA tools
// directory because that is the one tree Orobox owns: the project's checkout is the developer's
// (and in the pipeline a read-only clone), and OroRoot belongs to OroCommerce.
const mergedDir = config.QaToolsDir + "/merged"

// configRef is a tool's resolved configuration: Path is the shell expression naming the file the
// tool has to read, Setup the shell line that materialises that file when a merge is needed.
// Path is an expression rather than a string because whether the project ships its own config is
// only knowable inside the container, where the tools run.
type configRef struct {
	Path  string
	Setup string
}

// mergeRenderer renders the merged config that layers the project's file on top of the base one.
// Both arguments are absolute container paths, so the rendered document can reference them.
type mergeRenderer func(baseFile, projectFile string) string

// mergedConfig resolves the config file for one tool.
//
// A project config does not replace the base one, it layers on top of it: the base carries the
// shared standard (the ruleset algoritma/php-coding-standards generates, or the one OroCommerce
// ships for the JS tools) and the project's file adjusts it. Overriding instead would silently
// drop the whole standard the moment a project needed one extra exclude.
//
// With no renderer the older project-or-base fallback is kept. That is the right answer for the
// ignore files: their patterns are gitignore-style and resolve against the directory of the file
// holding them, so a merged copy written to a third directory re-anchors every pattern it
// inherited.
func mergedConfig(sourceRoot, baseDir, file string, render mergeRenderer) configRef {
	baseFile := baseDir + "/" + file
	projectFile := sourceRoot + "/" + file
	mergedFile := mergedDir + "/" + file

	// A project install roots its sources at OroRoot, so the JS tools' base and project configs
	// are the very same file and there is nothing to merge.
	if baseFile == projectFile {
		return configRef{Path: baseFile}
	}

	if render == nil {
		return configRef{Path: fmt.Sprintf("$([ -f %[1]s ] && echo %[1]s || echo %[2]s)", projectFile, baseFile)}
	}

	b64 := base64.StdEncoding.EncodeToString([]byte(render(baseFile, projectFile)))

	// The merged file is used only when both halves exist. That keeps every generated document
	// free of existence checks it cannot express — a NEON `includes` list or a YAML `extends` list
	// cannot skip a missing entry — and lets the PHP wrappers below fail on shape alone.
	return configRef{
		Path: fmt.Sprintf(
			"$(if [ -f %[1]s ] && [ -f %[2]s ]; then echo %[3]s; elif [ -f %[2]s ]; then echo %[2]s; else echo %[1]s; fi)",
			baseFile, projectFile, mergedFile),
		Setup: fmt.Sprintf(
			"if [ -f %[1]s ] && [ -f %[2]s ]; then mkdir -p %[3]s && printf '%%s' '%[4]s' | base64 -d > %[5]s; fi",
			baseFile, projectFile, mergedDir, b64, mergedFile),
	}
}

// joinSetup chains setup lines with && so a failed one stops the rest: a tool whose merged config
// was not written must not run against a stale or missing one.
func joinSetup(lines ...string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, " && ")
}

// phpstanConfigRef resolves PHPStan's configuration. Like Rector's it is always the generated
// wrapper, never the base or the project file directly, because Orobox has something of its own to
// contribute to it: the excludes that keep the analysis inside the code the project actually owns
// (see phpstanExcludedTrees). Falling back to either file as-is would drop those excludes exactly
// where they matter most — a bundle checkout with no phpstan.neon of its own, and the pipeline
// engine, which installs the tools itself and never runs `orobox qa-init` at all.
//
// The two configs are layered through NEON includes: later includes win on scalars and merge into
// the arrays of earlier ones, which is the precedence wanted, and relative paths inside either
// file keep resolving against that file, so neither has to know it was included. The list is built
// by the shell rather than written out here because a NEON `includes` list cannot skip a missing
// entry, and both halves are optional: the base one exists only once the tools are installed, the
// project's one only once a developer wrote it.
func phpstanConfigRef(sourceRoot, analyzePath string) configRef {
	baseFile := config.QaToolsDir + "/phpstan.neon"
	projectFile := sourceRoot + "/phpstan.neon"
	mergedFile := mergedDir + "/phpstan.neon"

	b64 := base64.StdEncoding.EncodeToString([]byte(phpstanExcludes(analyzePath)))

	return configRef{
		Path: mergedFile,
		Setup: fmt.Sprintf(
			"mkdir -p %[3]s && { if [ -f %[1]s ] || [ -f %[2]s ]; then echo 'includes:'; "+
				"if [ -f %[1]s ]; then echo '    - %[1]s'; fi; "+
				"if [ -f %[2]s ]; then echo '    - %[2]s'; fi; echo; fi; "+
				"printf '%%s' '%[5]s' | base64 -d; } > %[4]s",
			baseFile, projectFile, mergedDir, mergedFile, b64),
	}
}

// phpstanExcludedTrees are the directories under the analysed tree that hold code the project did
// not write. They are the same three the PHP-CS-Fixer and Twig-CS-Fixer stubs exclude from their
// finders (see scaffold.QaStubDataFor) — PHPStan takes an analysed path on the command line rather
// than a finder, so it needs the same list expressed as configuration.
//
// vendor-oro is the one that makes this a correctness matter rather than a speed one. A bundle
// checkout holds OroCommerce's whole installed tree under that name, so analysing the bundle root
// analyses the platform and every dependency it ships: PHPStan then reports thirteen findings
// against faker, behat/transliterator, gedmo, liip/imagine-bundle and symfony/cache — internal
// errors for optional classes those packages declare against and a PHP 8.4 fatal from
// doctrine/cache's Psr6 adapter, reached by autoload while analysing them — and closes with
// "Result is incomplete because of severe errors", which is a report on OroCommerce's vendors that
// says nothing about the bundle.
func phpstanExcludedTrees() []string {
	return []string{"vendor", "vendor-oro", "node_modules"}
}

// phpstanExcludes renders Orobox's own layer of the PHPStan config: the excluded trees under the
// analysed path.
//
// analyseAndScan rather than analyse, because the point is for PHPStan never to parse those files
// at all; the classes in them stay reachable through the `--autoload-file` composer autoloader,
// which is how PHPStan resolves every other symbol it does not index.
//
// Each entry ends in a wildcard so PHPStan reads it as a pattern and does not check that the
// directory exists. A project install analyses OroRoot/src, which holds none of these, and an
// exact path that is missing aborts the run with "excludePaths ... does not exist".
func phpstanExcludes(analyzePath string) string {
	var b strings.Builder

	b.WriteString("parameters:\n    excludePaths:\n        analyseAndScan:\n")
	for _, dir := range phpstanExcludedTrees() {
		fmt.Fprintf(&b, "            - %s/%s/*\n", analyzePath, dir)
	}

	return b.String()
}

// yamlExtendsMerge layers two ESLint or Stylelint configs through `extends`, which both tools
// accept as a list of config file paths, applied in order with the last one winning.
func yamlExtendsMerge(baseFile, projectFile string) string {
	return fmt.Sprintf("extends:\n  - %s\n  - %s\n", baseFile, projectFile)
}

// oroGeneratedSources are the files OroCommerce's own application skeleton writes into the tree
// the PHP tools walk, and that a project is not supposed to hand-maintain.
//
// src/AppKernel.php is the whole list today. It ships with `array()` literals, fully qualified
// bundle names written inline and no return types, so Rector reports it on every single run of
// every project — the tool's first finding on a brand-new checkout is a file the developer did
// not write and must not reformat, because the next OroCommerce release ships its own version of
// it again.
//
// The paths are absolute container paths: Rector runs from OroRoot and PHP-CS-Fixer's finder is
// rooted wherever the project config points it, so a relative path would mean two different files.
func oroGeneratedSources() []string {
	return []string{config.OroRootDir + "/src/AppKernel.php"}
}

// phpUnitAttributeRules are the PHP-CS-Fixer rules Orobox turns off for every project, because
// what they produce does not run on the PHPUnit the application ships.
//
// php_unit_attributes rewrites PHPUnit's docblock annotations into the PHPUnit 10 attributes and
// removes the annotation it replaced — `@dataProvider giveMeData` becomes
// `#[PHPUnit\Framework\Attributes\DataProvider('giveMeData')]`. Every Oro line Orobox supports
// pins PHPUnit 9.6 (oro/commerce-crm-application 7.0.3 requires phpunit/phpunit ~9.6.33 for
// development), and PHPUnit 9 reads neither of those attributes: the annotation is gone, the
// attribute means nothing, and the test that had a data provider now runs once with no arguments —
// or errors on the missing arguments. The rule arrives through the shared standard's
// @PHPUnit100Migration:risky set, so a project cannot avoid it by not asking for it.
//
// The rules are layered between the base standard and the project config, so a project that has
// moved to PHPUnit 10 takes them back simply by naming them in its own .php-cs-fixer.dist.php.
func phpUnitAttributeRules() []string {
	return []string{"php_unit_attributes"}
}

// phpDisabledRules renders rule names as a PHP array body of rules set to false, ready to be
// interpolated between brackets.
func phpDisabledRules(rules []string) string {
	disabled := make([]string, 0, len(rules))
	for _, rule := range rules {
		disabled = append(disabled, "'"+rule+"' => false")
	}
	return strings.Join(disabled, ", ")
}

// phpList renders paths as a PHP array body, ready to be interpolated between brackets.
func phpList(paths []string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "'"+path+"'")
	}
	return strings.Join(quoted, ", ")
}

// rectorConfigRef resolves Rector's configuration. Unlike the other tools it is always the generated
// wrapper, never the base or the project file directly, because Orobox has something of its own to
// contribute to it: the skip list for the sources OroCommerce generates (see oroGeneratedSources).
// Falling back to either file as-is would drop that skip exactly in the two cases where it matters
// most — a checkout with no rector.php of its own, which is what a project looks like before
// `orobox qa-init`, and the pipeline engine, which installs the tools itself and never runs
// qa-init at all.
//
// Both files are therefore optional here, and a missing one is skipped rather than fatal.
func rectorConfigRef(sourceRoot string) configRef {
	baseFile := config.QaToolsDir + "/rector.php"
	projectFile := sourceRoot + "/rector.php"
	mergedFile := mergedDir + "/rector.php"

	b64 := base64.StdEncoding.EncodeToString([]byte(rectorMerge(baseFile, projectFile)))

	return configRef{
		Path:  mergedFile,
		Setup: fmt.Sprintf("mkdir -p %s && printf '%%s' '%s' | base64 -d > %s", mergedDir, b64, mergedFile),
	}
}

// rectorMerge wraps both Rector configs in one closure that applies them in turn to the same
// RectorConfig, so the project's calls land on top of the base's, and adds Orobox's own skip list
// last.
//
// The two shapes a Rector config file can have are both handled: the older `return static
// function (RectorConfig $c)` and the current `return RectorConfig::configure()->...`, whose
// builder is invokable with the config. Anything else throws instead of being ignored — a config
// that was silently skipped is a QA run that reports fewer findings than it should.
//
// The skip goes last only for readability: Rector's skip list is additive, so a project cannot
// take an entry back out whatever the order is. A project that really wants Rector to rewrite the
// generated kernel has to run the tool on that file itself.
func rectorMerge(baseFile, projectFile string) string {
	return fmt.Sprintf(`<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;

return static function (RectorConfig $rectorConfig): void {
    foreach (['%s', '%s'] as $file) {
        if (!is_file($file)) {
            continue;
        }

        $config = require $file;

        if (is_callable($config)) {
            $config($rectorConfig);

            continue;
        }

        throw new RuntimeException(sprintf(
            'Orobox cannot merge the Rector config %%s: expected a closure or a RectorConfigBuilder, got %%s.',
            $file,
            get_debug_type($config)
        ));
    }

    // Written by OroCommerce, not by the project. See oroGeneratedSources.
    $rectorConfig->skip([%s]);
};
`, baseFile, projectFile, phpList(oroGeneratedSources()))
}

// phpCSFixerMerge merges the two PHP-CS-Fixer configs rule by rule, the project's winning on the
// rules it names, with Orobox's own rule overrides in between (see phpUnitAttributeRules).
//
// Everything that is not a rule — the finder, the cache file, the indentation — comes from the
// project config, because Config::getFinder() lazily builds a default one and "the project set a
// finder" is therefore not detectable. Risky rules are allowed when either side allows them: the
// merged rule set contains both sides' rules, so the stricter flag would make the tool refuse the
// other side's.
//
// The finder is then narrowed by the same exclusion Rector gets (see oroGeneratedSources): the
// project finder is what points the tool at src/, so it is also what walks into the kernel
// OroCommerce generated. The narrowing is a filter on absolute paths rather than a notPath()
// pattern because notPath() is relative to the finder's roots, and those are the project's to
// choose. It is guarded by the instanceof: a Config may answer with any iterable, and one that is
// not a Finder has no filter() to call.
func phpCSFixerMerge(baseFile, projectFile string) string {
	return fmt.Sprintf(`<?php

declare(strict_types=1);

use PhpCsFixer\ConfigInterface;
use Symfony\Component\Finder\Finder;

$load = static function (string $file): ConfigInterface {
    $config = require $file;

    if (!$config instanceof ConfigInterface) {
        throw new RuntimeException(sprintf(
            'Orobox cannot merge the PHP-CS-Fixer config %%s: expected a %%s, got %%s.',
            $file,
            ConfigInterface::class,
            get_debug_type($config)
        ));
    }

    return $config;
};

$base = $load('%s');
$project = $load('%s');

// See phpUnitAttributeRules: between the two sides, so the shared standard cannot switch the
// rules on and the project can still switch them back.
$project->setRules(array_merge($base->getRules(), [%s], $project->getRules()));
$project->setRiskyAllowed($base->getRiskyAllowed() || $project->getRiskyAllowed());

$finder = $project->getFinder();
if ($finder instanceof Finder) {
    $generated = [%s];

    $finder->filter(static function (\SplFileInfo $file) use ($generated): bool {
        return !in_array((string) $file->getRealPath(), $generated, true);
    });

    $project->setFinder($finder);
}

return $project;
`, baseFile, projectFile, phpDisabledRules(phpUnitAttributeRules()), phpList(oroGeneratedSources()))
}

// twigCSFixerMerge merges the two Twig-CS-Fixer rulesets into a fresh one, the project's rules
// applied after the base's. Rules are keyed by class so a rule both sides carry is added once,
// as the project's instance — and therefore with the project's configuration of it.
//
// The finder and the remaining Config settings come from the project config, for the same reason
// as in PHP-CS-Fixer above: a Config answers with a default rather than with nothing.
func twigCSFixerMerge(baseFile, projectFile string) string {
	return fmt.Sprintf(`<?php

declare(strict_types=1);

use TwigCsFixer\Config\Config;
use TwigCsFixer\Ruleset\Ruleset;

$load = static function (string $file): Config {
    $config = require $file;

    if (!$config instanceof Config) {
        throw new RuntimeException(sprintf(
            'Orobox cannot merge the Twig-CS-Fixer config %%s: expected a %%s, got %%s.',
            $file,
            Config::class,
            get_debug_type($config)
        ));
    }

    return $config;
};

$rules = [];
foreach (['%s', '%s'] as $file) {
    foreach ($load($file)->getRuleset()->getRules() as $rule) {
        if (!is_object($rule)) {
            throw new RuntimeException(sprintf(
                'Orobox cannot merge the Twig-CS-Fixer ruleset of %%s: expected rule objects, got %%s.',
                $file,
                get_debug_type($rule)
            ));
        }

        $rules[$rule::class] = $rule;
    }
}

$ruleset = new Ruleset();
foreach ($rules as $rule) {
    $ruleset->addRule($rule);
}

$project = $load('%s');
$project->setRuleset($ruleset);

return $project;
`, baseFile, projectFile, projectFile)
}
