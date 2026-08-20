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

// neonMerge layers two PHPStan configs through NEON includes. Later includes win on scalars and
// merge into the arrays of earlier ones, which is exactly the precedence wanted. Relative paths
// inside either file keep resolving against that file, so neither has to know it was included.
func neonMerge(baseFile, projectFile string) string {
	return fmt.Sprintf("includes:\n    - %s\n    - %s\n", baseFile, projectFile)
}

// yamlExtendsMerge layers two ESLint or Stylelint configs through `extends`, which both tools
// accept as a list of config file paths, applied in order with the last one winning.
func yamlExtendsMerge(baseFile, projectFile string) string {
	return fmt.Sprintf("extends:\n  - %s\n  - %s\n", baseFile, projectFile)
}

// rectorMerge wraps both Rector configs in one closure that applies them in turn to the same
// RectorConfig, so the project's calls land on top of the base's.
//
// The two shapes a Rector config file can have are both handled: the older `return static
// function (RectorConfig $c)` and the current `return RectorConfig::configure()->...`, whose
// builder is invokable with the config. Anything else throws instead of being ignored — a config
// that was silently skipped is a QA run that reports fewer findings than it should.
func rectorMerge(baseFile, projectFile string) string {
	return fmt.Sprintf(`<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;

return static function (RectorConfig $rectorConfig): void {
    foreach (['%s', '%s'] as $file) {
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
};
`, baseFile, projectFile)
}

// phpCSFixerMerge merges the two PHP-CS-Fixer configs rule by rule, the project's winning on the
// rules it names.
//
// Everything that is not a rule — the finder, the cache file, the indentation — comes from the
// project config, because Config::getFinder() lazily builds a default one and "the project set a
// finder" is therefore not detectable. Risky rules are allowed when either side allows them: the
// merged rule set contains both sides' rules, so the stricter flag would make the tool refuse the
// other side's.
func phpCSFixerMerge(baseFile, projectFile string) string {
	return fmt.Sprintf(`<?php

declare(strict_types=1);

use PhpCsFixer\ConfigInterface;

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

$project->setRules(array_merge($base->getRules(), $project->getRules()));
$project->setRiskyAllowed($base->getRiskyAllowed() || $project->getRiskyAllowed());

return $project;
`, baseFile, projectFile)
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
