[← Back to README](../README.md) · [Documentation index](README.md)

# QA tools

### 9. QA Tools Initialization (`qa-init`)
Configures and installs the necessary QA tools (PHPStan, coding standards, ESLint, Stylelint) in your bundle.
```bash
orobox qa-init
```

ESLint and Stylelint are the exception to "installs": Orobox runs OroCommerce's own binaries from
`/var/www/oro/node_modules/.bin`, in both `type: bundle` and `type: project`. The configuration
those two linters are handed is OroCommerce's — `.eslintrc.yml` and `.stylelintrc.yml` at the
application root, extending packages its `package.json` declares — so the only version guaranteed
to satisfy it is the one the application installed for `npm run eslint-oro`. `qa-init` therefore
installs only what the application does not provide itself: the two GitLab Code Quality formatters,
plus the ESLint plugins and shareable config OroCommerce's own `.eslintrc.yml` names but its
generated `package.json` does not install (`eslint-plugin-no-jquery` is one). Those fillers are
symlinked into `/var/www/oro/node_modules` at run time, and only where the application has nothing
of its own there — nothing is ever written to its `package.json`.

The linters appear with the application's `node_modules`, which the asset install populates. In CI
the QA step installs them itself when they are not there — a cached or database-seeded Oro install
never runs the asset install — so nothing extra is needed there. On a developer's stack, `orobox qa`
stops with the reason when they are missing: build the assets once
(`php bin/console oro:assets:install`).

`qa-init` also writes a configuration stub for each enabled tool into your source root, when the
file is not there yet. The stubs are yours from the first write on: Orobox layers them on top of the
shared standard rather than replacing it — see
[Project configuration files](#project-configuration-files) — so what you put in them wins without
repeating the standard.

| Stub | Written for |
| --- | --- |
| `phpstan.neon`, `rector.php`, `.php-cs-fixer.dist.php`, `.twig-cs-fixer.php` | every install type |
| `.eslintrc.yml`, `.stylelintrc.yml`, `.stylelintrc-css.yml` | `type: bundle` only |

The JS stubs are bundle-only because a project install roots its sources at the same directory as
OroCommerce's own copies of those files, so a stub there would replace them rather than extend them.
The ignore files (`.eslintignore`, `.stylelintignore`) are never generated for the same reason: they
are chosen, not merged.

The finder in `.php-cs-fixer.dist.php` and `.twig-cs-fixer.php` is the tree those tools actually
walk — the merge takes rules from both sides but the finder from yours. Edit it rather than deleting
it.

### 10. Run QA Tools (`qa`)
Executes the QA analysis tools. When no flag is provided, runs the tools enabled in `.orobox.yaml` under `test.qa` (all tools are enabled by default if not configured).
```bash
orobox qa
```
Options:
- `--phpstan`: Run PHPStan analysis.
- `--rector`: Run Rector process.
- `--php-cs-fixer`: Run PHP-CS-Fixer fix.
- `--twig-cs-fixer`: Run Twig-CS-Fixer lint.
- `--eslint`: Run ESLint analysis.
- `--stylelint`: Run Stylelint analysis.
- `--engine=compose|dagger`: Where the checks run. See [Running the checks in CI](#running-the-checks-in-ci).
- `--report=gitlab`: Write a GitLab Code Quality report.
- `--report-path`: Where to write it (default `var/orobox/reports/code-quality.json`).
- `--cache-scope`, `--base-cache-scope`: Dagger engine only; same meaning as on `orobox deploy`.
- `--generate-baseline`: Record PHPStan's current findings instead of failing on them. See
  [The PHPStan baseline](#the-phpstan-baseline).

CLI flags always override the configuration. Example:
```bash
orobox qa --phpstan --eslint
```

#### The PHPStan baseline

An existing codebase usually has more findings than anyone is going to fix before the next commit.
A baseline is how PHPStan is turned on anyway: the findings that exist today are recorded in a file
and ignored from then on, so a run fails only on what was added after it.

```bash
orobox qa --generate-baseline
```

The command runs PHPStan alone — that is the only tool with a baseline — writes
`phpstan-baseline.neon` next to your `phpstan.neon`, and adds it to that file's `includes` so the
next `orobox qa` reads it. Both files belong in the repository: without the baseline every developer
and every pipeline sees the whole backlog again.

Run it again after fixing some of the findings and the baseline shrinks to what is still there;
PHPStan leaves the file it is regenerating out of its own configuration, so a refresh reports the
tree as it is now rather than coming back empty.

Two combinations are refused rather than half-honoured:

- `--engine=dagger`, because that engine analyses a throwaway clone of a git ref — the baseline it
  wrote would never reach the checkout that has to commit it.
- `--report=gitlab`, because a baseline run records the findings in NEON instead of reporting them,
  so there would be nothing to put in the report.

PHPStan boots the real application kernel — phpstan-symfony enumerates the console commands,
phpstan-doctrine asks Doctrine for the entity manager — so the loaders Orobox generates in
`vendor-bin/qa/tests/` boot the dotenv files first, the way `bin/console` does through
symfony/runtime. The file they read is the one `composer.json` names in
`extra.runtime.dotenv_path` (Oro renames it to `.env-app`), so `%env(...)%` placeholders resolve to
the same values a console command would see. Without that, Doctrine falls back to its default host
and the analysis dies with `connection to server at "127.0.0.1" ... refused`.

PHPStan reads a dumped debug container, so it needs the matching environment installed. On a
developer's machine the tools run in `dev`, the environment `orobox install` already set up, and
warm `var/cache/dev`. In CI (the `CI` environment variable set) they run in `test` instead, matching
the install the pipeline performs, and warm `var/cache/test`. The warmed cache is reused by later
runs.

#### The shared vendor tree

The QA tools live in their own Composer tree, `vendor-bin/qa`, so their versions never touch the
application's. Every package both trees need — `symfony/console`, the Symfony contracts, `psr/log`,
`twig/twig` and the rest — is installed **once**, in the application's tree, and reached from the QA
tree through the bootstrap Orobox writes to `vendor-bin/qa/orobox/oro-autoload.php`.

That is not a size optimization. A dumped Symfony debug container inline-requires vendor files by
absolute path, and `include_once` dedupes on the path, not on the class name. With two copies
installed, PHPStan boots the kernel, the container requires the application's copy of a class the
QA copy already declared, and the run dies with:

```
Fatal error: Cannot redeclare interface Symfony\Contracts\Service\ResetInterface
```

Twig fails the same way through a global function rather than a class — `Cannot redeclare
twig_var_dump()`, declared by the `Resources/debug.php` that `DebugExtension` requires — which is
why `twig/twig` is shared too. Twig dropped those functions in 3.9, so only Oro lines still on an
older Twig reach that particular fatal.

Identical versions in both trees do not help — the paths still differ. So `orobox qa-init` (and the
deploy pipeline's QA stage) patches `vendor-bin/qa/composer.json` before Composer populates it:
each shared package the application ships is written into `replace` at the patch line installed
there (`6.4.*` for a `v6.4.17`) and removed from the QA requirements. A package the application
does **not** ship keeps its pinned requirement and is installed in the QA tree as before.

The line rather than the point release, because Composer answers an unsatisfiable requirement by
installing an older release of whoever asked for it, not by failing: an exact `6.4.16` against
PHP-CS-Fixer's `symfony/options-resolver: ^6.4.24` quietly pulled in an
`algoritma/php-coding-standards` old enough that its Composer plugin never wrote the shared
ruleset, and the QA run then analysed without it. Symfony keeps its patch releases BC, so the
line is a claim the application's copy can honour.

Replacing the shared packages is not the whole fix, because the two trees always overlap somewhere
Orobox cannot predict — `twig/twig` arrives in the QA tree through Twig-CS-Fixer, `sebastian/diff`
through PHP-CS-Fixer, and both live in the application's tree too. What decides the outcome is
which tree the process resolves a duplicated class from, and that differs per tool:

- **PHPStan** boots the application kernel, so every class the dumped container inline-requires
  has to come from the application's tree. It runs with the application's autoloader **first**.
- **Every other tool** never loads that container, so the QA tree stays first and the tools keep
  dependency versions the application does not have — PHP-CS-Fixer's `sebastian/diff` is five
  majors ahead of Oro's copy.

The bootstrap reads which order to use from an environment variable Orobox sets on the PHPStan
command line only, and it is loaded through `autoload.files` — the earliest hook Composer has, so
no tool code can pin the process to the wrong copy first.

The patch edits your committed manifest, so commit the result together with the refreshed
`vendor-bin/qa/composer.lock`. The run that first applies it re-resolves the QA tree once; later
runs are lock-driven again.

#### Project configuration files

Every tool runs against a base configuration Orobox owns: the ruleset
`algoritma/php-coding-standards` generates in `vendor-bin/qa` for the PHP tools, the one OroCommerce
ships at the application root for the JS ones. A configuration file of the same name in your source
root is **merged on top of that base** rather than replacing it, so adding one exclude no longer
drops the whole shared standard. Your file wins wherever the two disagree.

`algoritma/php-coding-standards` writes its files from a Composer plugin event that not every
release fires — the older releases the shared-vendor pins resolve to on Oro 6.0 install silently
without one — so the QA install asks for them explicitly afterwards
(`composer algoritma-phpstan-create-config` and its Rector and PHP-CS-Fixer siblings), and never
overwrites a file that is already there.

| File | How it merges |
| --- | --- |
| `phpstan.neon` | NEON `includes`: base first, yours last, then Orobox's own excludes. Relative paths in either file keep resolving against that file. |
| `rector.php` | Both configs are applied to the same `RectorConfig`, base first, then Orobox's own skip list. Either shape works — a `static function (RectorConfig $c)` or a `RectorConfig::configure()` builder. |
| `.php-cs-fixer.dist.php` | Rules merge key by key, yours winning, with Orobox's PHPUnit override in between (see below). Risky rules are allowed when either side allows them. Finder, cache file and indentation come from your config, minus the generated sources below. |
| `.twig-cs-fixer.php` | Rulesets merge rule by rule (keyed by rule class), yours winning. Finder and the remaining `Config` settings come from your config. |
| `.eslintrc.yml`, `.stylelintrc.yml`, `.stylelintrc-css.yml` | `extends`: base first, yours last. |
| `.eslintignore`, `.stylelintignore`, `.stylelintignore-css` | **Not merged** — yours replaces the base one. Ignore patterns resolve against the directory of the file holding them, so a merged copy would re-anchor every inherited pattern. |

The merged file is generated into `vendor-bin/qa/merged/` on every run; nothing is written to your
checkout. When you ship no config of your own, the base one is used directly — except for
`rector.php` and `phpstan.neon`, which always go through the generated wrapper because Orobox has
something of its own to add to each: a skip list for Rector, the excluded trees for PHPStan (see
below).

**Vendored trees are excluded from the analysis.** `vendor`, `vendor-oro` and `node_modules` hold
code the project did not write, so PHPStan's `excludePaths` drops them and the PHP-CS-Fixer and
Twig-CS-Fixer finders never walk them. `vendor-oro` is why this is a correctness matter and not a
speed one: a bundle checkout holds OroCommerce's whole installed tree under that name, so analysing
the bundle root analysed the platform and every dependency it ships — thirteen findings against
faker, gedmo, symfony/cache and friends, closing with "Result is incomplete because of severe
errors", none of it about the bundle.

**`php_unit_attributes` is off.** The shared standard enables PHP-CS-Fixer's
`@PHPUnit100Migration:risky` set, whose `php_unit_attributes` rule rewrites PHPUnit's docblock
annotations into PHPUnit 10 attributes and deletes the annotation it replaced — `@dataProvider
giveMeData` becomes `#[PHPUnit\Framework\Attributes\DataProvider('giveMeData')]`. Every Oro line
pins PHPUnit 9.6, which reads neither, so the rewritten test loses its data provider. Orobox turns
the rule off between the base standard and your config: name it in your own
`.php-cs-fixer.dist.php` to take it back once your tests run on PHPUnit 10.

**Sources OroCommerce generates are excluded.** `src/AppKernel.php` is written by the OroCommerce
application skeleton and shipped again with every release, so reformatting it is work the next
update undoes. Rector skips it and PHP-CS-Fixer's finder drops it, on every install type. Nothing
else is excluded: the skip list is the file OroCommerce owns, not a general opt-out.

A config Orobox cannot merge — a `rector.php` returning neither a closure nor a builder, a
`.php-cs-fixer.dist.php` returning something that is not a `PhpCsFixer\ConfigInterface` — fails the
tool with a message naming the file and what it actually returned. It is never silently ignored.

### Running the checks in CI

`orobox qa` and `orobox test` can run on either of two engines:

- **compose** — the development stack, through `docker compose exec`. The default on a developer's
  machine.
- **dagger** — the deploy pipeline's own engine: the same tool invocations, on the same cache
  volumes, against the same cached Oro install. The default when the `CI` environment variable is
  set and the install type is `project`.

`--engine` forces either one. The Dagger engine is available only for `type: project`, because the
pipeline image (`algoritmadev/orobox:<version>-project-latest`) exists only for projects. It does
**not** require a `deploy` section in `.orobox.yaml`: a project that never deploys with Orobox can
still use it in CI.

In CI the sources are taken from the job's own checkout (`CI_PROJECT_DIR`), never cloned again.
Everything the repository ignores is excluded from the upload — that is not just an optimization: a
local `vendor/` carrying dev dependencies would otherwise be overlaid on top of the pipeline's own.

The cache volume family defaults to the current branch (`CI_COMMIT_REF_NAME`, or
`git rev-parse --abbrev-ref HEAD` locally), so a lint job reuses what the deploy of that branch
built, and two branches with different migrations do not invalidate each other.

#### Reports

`--report=gitlab` makes every tool emit its own report; Orobox merges them into one document. Most
of them speak GitLab Code Quality natively — ESLint through the `eslint-formatter-gitlab` package
`orobox qa-init` installs, the linters themselves coming from OroCommerce's `node_modules`, see
[QA Tools Initialization](#9-qa-tools-initialization-qa-init). Two are converted by Orobox: Rector,
whose `gitlab` format prints a deprecation warning into the report and is being removed anyway, so
it reports its own JSON; and stylelint, which is handed a formatter Orobox writes, because the
published ones are each pinned to a stylelint major and the major that runs is OroCommerce's
choice.

With `--report`, the QA tools no longer stop at the first failure — a Code Quality report listing
only PHPStan's findings because Rector never ran would be worse than none. Every tool runs, and the
command still exits non-zero if any of them found something.

The untouched per-tool files are kept under `var/orobox/reports/raw/`. When a merged report looks
wrong, they are what says whether the tool or the merge is to blame.

#### GitLab CI

`orobox ci-init` generates the pipeline — see
[CI Initialization (`ci-init`)](deployment.md#14-ci-initialization-ci-init). The generated jobs run
`orobox qa --report=gitlab`, `orobox test --report=gitlab` and `orobox deploy <stage>`, and publish
the reports at the paths those commands write to.

`--skip-qa --skip-test` on the generated deploy job is sound in a way it would not be otherwise:
the lint and test jobs ran the same code, on the same commit, against the same cache scope.

Note that `orobox deploy` in CI also builds from `CI_PROJECT_DIR` rather than cloning, and the
release therefore checks out the commit that was built (`CI_COMMIT_SHA`) rather than the stage's
configured `ref`. An explicit `--ref` still wins. This is what keeps the deployed revision equal to
the tested one.

All of this depends on the Dagger engine's cache surviving between jobs — see
[Cache warmth in CI](deployment.md#cache-warmth-in-ci).
