# Orobox E2E Test Suite

End-to-end tests that provision a **real** OroCommerce environment for every
supported version and install type, then exercise the green ("happy") path of
every user-facing Orobox command against it. This complements the unit tests;
it does not replace them.

Design spec: [`docs/superpowers/specs/2026-08-24-e2e-test-suite-design.md`](../docs/superpowers/specs/2026-08-24-e2e-test-suite-design.md)

## What it covers

- **Versions:** all of `config.SupportedOroVersions` (`7.0`, `6.1`, `6.0`, `5.1`).
- **Install types:** `project` and `bundle`.
- **Commands (green path):** `init`, `up`, `run`, `console`, `db backup`,
  `db restore`, `create bundle`, `qa`, `test`, `logs`, `xdebug on|off|status`,
  the generators (`deploy-init`, `ci-init`, `qa-init`), `test-init`, `clear`,
  `down`.
- `run` invokes a custom command the fixtures define under `commands:`; it is
  not a console passthrough (that is `console`).
- `deploy-init` and `ci-init` are project-only by design, so for `bundle` the
  suite asserts that orobox refuses them instead of expecting files.
- `create bundle` runs inside the project checkout, before `qa` and `test`, so
  the shipped skeleton is analysed by the project's own tools and not only by
  the unit tests. What the step proves is that the generated bundle is one the
  application actually loads: the namespace resolves through the application's
  `"": "src/"` PSR-4 rule, Oro's kernel discovers the generated
  `Resources/config/oro/bundles.yml`, and the generated `Extension` and
  `Configuration` agree on an alias — asserted by a `console cache:clear`
  followed by the bundle's alias appearing in `console debug:config`. All three
  fail silently otherwise: the files are still written, just somewhere inert.
  It is skipped for `bundle`, whose fixture maps its PSR-4 prefix to the
  package root; `create` there falls back to a standalone package, which
  `create_test.go` covers host-side without polluting the checkout under test.
- `test-init` provisions the test database rather than writing files, so it is
  asserted to complete rather than to generate anything.
- `qa` runs **after** `qa-init` and in report mode (`--report gitlab`). Report
  mode is what keeps every tool running: without it the tools are chained with
  `&&`, so the first one with findings — usually PHPStan — silences Rector,
  PHP-CS-Fixer and the rest, and the suite learns nothing about them.
- `test` runs **after** `test-init` (without the test database `orobox test`
  never reaches PHPUnit) and is **narrowed**: one run per suite (`unit`,
  `functional`), each with a filter that matches a handful of tests instead of
  the whole OroPlatform/OroCommerce suite, which takes hours and says nothing
  about orobox. The filter differs per install type (`Case.TestFilter`):
  `UserTest` for a project, which runs Oro's own tests, and `E2ETest` for a
  bundle, where PHPUnit is pointed at the bundle's own configuration and the
  only suites that exist are the fixture checkout's. The step is graded from the JUnit report
  (`--report gitlab`), not from the exit code: PHPUnit exits 0 when a filter
  matches nothing, so the report's counts are what separate a real pass from an
  empty run.

### Grading

- **Hard gate** (failure fails the case): `init`, `up` (must serve HTTP 200 on
  storefront and `/admin`), `run`, `console`, `db backup`, `db restore`, the
  generators, `test-init`, `clear`, `down`.
- **Best-effort** (logged, never fails the case): a `test` run that could not
  execute anything at all, and QA findings. A fresh community install of an
  older version may not support the current PHPUnit tooling; when unsupported
  the step is logged and skipped, when supported it must run green.
- **Hard gate inside the best-effort step:** once `test` does execute, it must
  execute something and it must pass — a report with zero tests, or with
  failures or errors, fails the case.
- **Hard gate inside `qa`:** every enabled tool must be installed, configured
  and actually invoked. What it *finds* does not fail the case — findings are a
  property of Oro's own code and of the tools' rulesets, not of orobox — but a
  tool that could not run does. The two are told apart per tool: a non-zero exit
  next to a report holding findings is lint, while a non-zero exit with an empty
  or unreadable report is an installation or configuration failure.

### Create, host-side

`create_test.go` covers the two `create` subcommands without Docker, since
neither touches it:

- `create bundle` standalone (no project composer.json) — the full skeleton,
  a `composer.json` that parses and declares the right PSR-4 prefix, the
  non-empty-target refusal, and `--path`.
- `create bundle` inside a PSR-4 project — the namespace decides the directory
  and the bundle carries no composer.json of its own.
- `create project` — the one create case that reaches the network. It clones
  the public OroCommerce application skeleton, asserts the `.git` strip and the
  non-empty-target refusal, and pins the premise the placement rule rests on:
  that the application's own composer.json maps `""` to `src/`. Set
  `E2E_SKIP_CLONE` to skip it on a machine without network.

### Intentionally excluded

- Real remote `deploy` (needs a remote host and credentials).
- `self-update` (mutates the binary under test).
- The `demo` install type (may be added later using the same harness).

## Prerequisites

- **Docker** and **Docker Compose**.
- Enough **disk space** for multiple OroCommerce installs (tens of GB across the
  full matrix).
- **`GITHUB_TOKEN`** (or a full **`COMPOSER_AUTH`** JSON) exported into the
  environment, so composer install does not hit GitHub's anonymous rate limit.
- **mkcert** optional — SSL is disabled in the fixtures.
- The case domains must resolve to the local stack. In CI they are mapped to
  `127.0.0.1` via `/etc/hosts`; locally add entries like
  `127.0.0.1 oro-project-61.e2e.test oro-bundle-61.e2e.test` for the cases you
  run.

## Running

Full matrix (build + all versions × both types — very long):

```bash
make e2e
```

A single fast case:

```bash
make build
OROBOX_BIN=$PWD/orobox E2E_VERSIONS=6.1 E2E_TYPES=bundle \
  go test -tags e2e -timeout 60m -v ./e2e/...
```

### Environment variables

- `OROBOX_BIN` — path to the compiled `orobox` binary. If unset, the suite
  builds one once via `go build`.
- `E2E_VERSIONS` — comma list of versions (default: all supported).
- `E2E_TYPES` — comma list of install types (default: `project,bundle`).
- `E2E_SKIP_CLONE` — skip `TestCreateProjectClonesTheOroApplication`, the only
  create case that needs the network.
- `GITHUB_TOKEN` / `COMPOSER_AUTH` — forwarded to the containers that run
  composer.

## CI

A dedicated `e2e` job in `.github/workflows/ci.yml` runs nightly (cron) and on
manual `workflow_dispatch`, one job per version (`fail-fast: false`). It does
**not** run on pull requests — four full installs are too slow to gate merges.

The job archives `E2E_LOG_DIR`, which per case holds one log per orobox
invocation, the generated compose configuration under `orobox-config/`, and the
per-tool QA and test files under `reports-raw/` — one status and one report per
tool, exactly as the tool wrote them. The last one is not redundant with the
step logs: the Dagger engine renders a step through a progress pane that keeps a
bounded number of lines, so the tail of a QA step that ran seven tools is
routinely not in the log at all.

## Layout

- `support.go` — pure helpers (matrix parsing, config rendering, project-name
  derivation, binary resolution); unit-tested under the normal build.
- `support_test.go` — unit tests for the helpers and fixtures.
- `harness.go` (`//go:build e2e`) — the `Box` harness: isolated workdir, binary
  execution, HTTP checks, guaranteed Docker teardown.
- `e2e_test.go` (`//go:build e2e`) — the matrix driver and green-path sequence.
- `create_test.go` (`//go:build e2e`) — the `create` cases that need no Docker:
  bundle scaffolding (standalone and inside a PSR-4 project) and the project
  clone.
- `fixtures/` — `text/template` `.orobox.yaml` configs for each install type,
  plus `fixtures/bundle/`, the checkout a bundle case starts from: a
  `composer.json`, a bundle class and a `phpunit.xml.dist` with a `unit` and a
  `functional` suite. It is copied into the case working directory before
  `.orobox.yaml` is written. A bundle checkout is the developer's own
  repository and orobox never writes its sources, so without it the suite
  exercised a bundle no developer could have: `orobox init` skipped the
  `composer require` that installs the bundle into the application,
  PHP-CS-Fixer aborted with "Unable to find composer.json" and PHPUnit found no
  configuration and wrote an empty JUnit log for every suite.
