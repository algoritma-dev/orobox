# Captured QA tool output

The fixtures in this directory are the real GitLab Code Quality output of the six QA tools, taken
from an OroCommerce 5.1 project on 2026-08-18. They exist so the merge in `codequality.go` is
tested against what the tools actually emit rather than against what their documentation suggests.

The tools ran from `/var/www/oro` inside the application container, against a scratch directory of
deliberately broken files at `var/qa-probe/`.

## Tool versions

| Tool | Version |
| --- | --- |
| PHPStan | 2.2.8 |
| Rector | 2.6.1 |
| PHP CS Fixer | 3.94.2 |
| Twig-CS-Fixer | 4.0.2 |
| ESLint | 8.57.1 |
| Stylelint | 15.11.0 |
| eslint-formatter-gitlab | 5.1.0 |
| stylelint-formatter-gitlab | 1.0.2 |

## How each report is requested

| Tool | Flag | Where the JSON goes |
| --- | --- | --- |
| PHPStan | `--error-format=gitlab` | stdout |
| Rector | `--output-format=gitlab` | stdout |
| PHP CS Fixer | `--format=gitlab` | stdout |
| Twig-CS-Fixer | `--report=gitlab` | stdout |
| ESLint | `--format <abs path to eslint-formatter-gitlab>` | the file named by `ESLINT_CODE_QUALITY_REPORT` |
| Stylelint | `--custom-formatter <abs path to stylelint-formatter-gitlab>` | the file named by `STYLELINT_CODE_QUALITY_REPORT` |

## Three findings that contradict the original design

**1. The two JS formatters do not write to stdout.** They print their usual human-readable output
there and write the Code Quality document to the file named by an environment variable. Redirecting
their stdout into a report file therefore captures the wrong thing. They need the environment
variable set to the destination instead, and their stdout left alone — which is a bonus: unlike the
four PHP tools, their console output stays visible in the job log.

**2. `eslint --format gitlab` does not resolve the formatter.** ESLint 8 looks for
`lib/cli-engine/formatters/gitlab` inside its own installation and fails with
`Cannot find module`. The absolute path to the package works:
`--format /path/to/node_modules/eslint-formatter-gitlab`.

**3. `eslint-formatter-gitlab` must be pinned to `^5.1.0`.** Version 6 and later declare
`peerDependencies: {"eslint": ">=9"}`, and Orobox installs `eslint@^8.57.0`. Installing the
unpinned package makes `npm install` fail outright:

```
npm ERR! ERESOLVE unable to resolve dependency tree
npm ERR! Found: eslint@8.57.1
npm ERR! peer eslint@">=9" from eslint-formatter-gitlab@7.2.0
```

## Path format

Every one of the six reports paths relative to the working directory the tool ran in
(`var/qa-probe/Bad.php`, not `/var/www/oro/var/qa-probe/Bad.php`). PHP CS Fixer escapes the
separators (`var\/qa-probe\/Bad.php`), which is only JSON encoding and decodes to the same string.

The merge keeps the absolute-path branch anyway, as a cheap defence against a tool or a version
that reports differently. What it must always do is prefix `deploy.source_dir` back on for a
monorepo: GitLab resolves the path against the repository root, which sits above the application.

## Exit codes when findings exist

| Tool | Exit code |
| --- | --- |
| PHPStan | 1 |
| Rector | 2 |
| PHP CS Fixer | 8 |
| Twig-CS-Fixer | 1 |
| ESLint | 1 |
| Stylelint | 2 |

All non-zero, which is what makes the aggregating script necessary: chained with `&&`, the first
tool with a finding would stop every tool after it and the report would be silently partial.
