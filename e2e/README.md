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
  `db restore`, `qa`, `test`, `logs`, `xdebug on|off|status`, the generators
  (`deploy-init`, `ci-init`, `qa-init`), `test-init`, `clear`, `down`.
- `run` invokes a custom command the fixtures define under `commands:`; it is
  not a console passthrough (that is `console`).
- `deploy-init` and `ci-init` are project-only by design, so for `bundle` the
  suite asserts that orobox refuses them instead of expecting files.
- `test-init` provisions the test database rather than writing files, so it is
  asserted to complete rather than to generate anything.

### Grading

- **Hard gate** (failure fails the case): `init`, `up` (must serve HTTP 200 on
  storefront and `/admin`), `run`, `console`, `db backup`, `db restore`, the
  generators, `test-init`, `clear`, `down`.
- **Best-effort** (logged, never fails the case): `qa` and `test`. A fresh
  community install of an older version may not support the current QA tooling;
  when unsupported the step is logged and skipped, when supported it must run
  green.

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
- `GITHUB_TOKEN` / `COMPOSER_AUTH` — forwarded to the containers that run
  composer.

## CI

A dedicated `e2e` job in `.github/workflows/ci.yml` runs nightly (cron) and on
manual `workflow_dispatch`, one job per version (`fail-fast: false`). It does
**not** run on pull requests — four full installs are too slow to gate merges.

## Layout

- `support.go` — pure helpers (matrix parsing, config rendering, project-name
  derivation, binary resolution); unit-tested under the normal build.
- `support_test.go` — unit tests for the helpers and fixtures.
- `harness.go` (`//go:build e2e`) — the `Box` harness: isolated workdir, binary
  execution, HTTP checks, guaranteed Docker teardown.
- `e2e_test.go` (`//go:build e2e`) — the matrix driver and green-path sequence.
- `fixtures/` — `text/template` `.orobox.yaml` configs for each install type.
