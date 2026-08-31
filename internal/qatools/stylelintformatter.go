package qatools

import (
	"encoding/base64"
	"fmt"
)

// stylelintFormatterFile is the formatter Orobox writes into the QA tools directory and hands
// stylelint through --custom-formatter.
//
// It exists because the published GitLab formatters are pinned to a stylelint major and the
// stylelint that runs is not Orobox's to choose. The linters are OroCommerce's own installation
// (see BinaryPaths), so their major is whatever the application declared, which differs per Oro
// line, per LTS patch and per project — while `stylelint-formatter-gitlab` calls stylelint's
// `formatters[name]` as a function, which 16 turned into a promise, and `@studiometa`'s
// replacement declares a `^16 || ^17` peer and does the reverse. Picking one from a version table
// produced exactly the crash it was meant to avoid:
//
//	TypeError: formatters[STYLELINT_FORMATTER] is not a function
//
// A formatter is a function of the results, so this one depends on no stylelint API at all and
// works whatever major loads it. It is CommonJS with a .cjs extension: stylelint 15 `require()`s
// the path and 16 and later `import()` it and take the default export, which for CommonJS is
// module.exports either way, and the explicit extension keeps that true under a manifest declaring
// "type": "module".
const stylelintFormatterFile = mergedDir + "/orobox-stylelint-gitlab.cjs"

// stylelintFormatterSource is the formatter itself.
//
// It writes the document to stdout, unlike the packages it replaces, which write it to the file
// named by an environment variable and keep the console output on stdout. That makes stylelint the
// same shape as the PHP tools — a redirect is the whole report mechanism — and it is why
// reportEnvByTool no longer names the two stylelint tools.
const stylelintFormatterSource = `'use strict';

// Written by Orobox. The GitLab Code Quality document is stdout; nothing else is printed there.
const { createHash } = require('crypto');

// GitLab's severities are its own vocabulary: stylelint's "error" is a rule the project decided
// must not appear, and its "warning" is one it wants to know about.
const SEVERITIES = { error: 'major', warning: 'minor' };

function issue(path, line, rule, text, severity) {
    // The fingerprint is what GitLab deduplicates on across pipelines, so it has to follow the
    // finding rather than its position in the list.
    const fingerprint = createHash('sha256')
        .update([path, rule, line, text].join(';'))
        .digest('hex');

    return {
        fingerprint,
        type: 'issue',
        categories: ['Style'],
        severity: SEVERITIES[severity] || 'minor',
        description: text,
        check_name: rule,
        location: {
            path,
            lines: { begin: line || 0 },
        },
    };
}

module.exports = function oroboxGitlabFormatter(results) {
    const issues = [];

    for (const result of results || []) {
        // Absolute inside the container; the report merge rewrites it to a repository path.
        const path = result.source || '';

        for (const warning of result.warnings || []) {
            issues.push(issue(path, warning.line, warning.rule || 'stylelint', warning.text, warning.severity));
        }
        // A file stylelint could not parse is not a clean file, and neither is a rule it was
        // configured with wrongly. Both are reported rather than dropped, because a report that
        // omits them reads as a tree with nothing wrong in it.
        for (const parseError of result.parseErrors || []) {
            issues.push(issue(path, parseError.line, parseError.stylelintType || 'parseError', parseError.text, 'error'));
        }
        for (const invalidOption of result.invalidOptionWarnings || []) {
            issues.push(issue(path, 0, 'invalidOption', invalidOption.text, 'error'));
        }
    }

    return JSON.stringify(issues);
};
`

// stylelintFormatterRef is the formatter's path and the shell line that writes it, in the shape
// every other generated file in this package uses.
func stylelintFormatterRef() configRef {
	b64 := base64.StdEncoding.EncodeToString([]byte(stylelintFormatterSource))

	return configRef{
		Path:  stylelintFormatterFile,
		Setup: fmt.Sprintf("{ mkdir -p %s && printf '%%s' '%s' | base64 -d > %s; }", mergedDir, b64, stylelintFormatterFile),
	}
}
