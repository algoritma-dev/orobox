# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versioning follows
[SemVer](https://semver.org/). See [CONTRIBUTING.md](CONTRIBUTING.md#release-policy) for what
"release candidate" means for this project.

Entries below `1.0.0-rc1` are reconstructed and aggregated from `git log` for readability; they
group commits by theme rather than listing every commit.

## [Unreleased]

- Fixed `orobox qa --agent` and `orobox test --agent` printing the pipeline's progress on the
  Dagger engine. Agent mode was honoured everywhere Orobox writes for a human except in
  `internal/pipeline`, whose reporter wrote its step banners, each command's streamed output and
  its heartbeats straight to stdout — so an automated caller read `▸ [deps] composer install` and
  `--- Running phpstan ---` on the stream that promises one finding per line and nothing else.
  Only project installs were affected, because that is where the Dagger engine is the default in
  CI.
- Fixed the QA tool flags (`--phpstan`, `--rector`, `--php-cs-fixer`, `--twig-cs-fixer`,
  `--eslint`, `--stylelint`) being ignored on the Dagger engine, which filtered on `.orobox.yaml`
  alone: `orobox qa --php-cs-fixer` answered a one-tool question with every tool's findings. The
  selection now reaches both engines and means the same thing on each — the named tools run,
  whatever the configuration enables. A run narrowed away from PHPStan also skips the QA warmup,
  the Oro install and test-cache warm that only PHPStan reads.

## [1.0.1] - 2026-09-18

- Fixed `orobox self-update` installing a distribution package instead of the binary. The asset
  filter only skipped archives and checksums, so `orobox_1.0.0_linux_amd64.apk` — which contains
  both `linux` and `amd64` in its name and is listed before the raw binary — was downloaded and
  written over the executable, and the next run failed with `exec format error`. The update now
  matches the published binary by its exact name, falls back to an allowlist of executable file
  extensions rather than a list of extensions to skip, and verifies the download starts with an
  executable magic number before replacing the installed binary.

## [1.0.0] - 2026-09-17

- First stable release. It carries the same code as `1.0.0-rc34`; the release-candidate series
  that started at `1.0.0-rc1` ends here.
- `releases/latest` now resolves to a stable tag, so the README install commands use
  `releases/latest/download/` instead of pinning an explicit `rcN`, and `orobox self-update` and
  the update notice track stable releases.

## [1.0.0-rc34] - 2026-09-17

- Added `orobox qa --staged`, which checks only the files the current commit stages. Each tool is
  narrowed by extension and a tool with nothing staged for it does not run; PHPStan still analyses
  the whole tree, because a changed class breaks callers the commit never touched. A staged run is
  always check-only.
- Added a `pre-commit` hook offered at the end of `orobox qa-init`. It runs `orobox qa --staged`
  and then `orobox test`, the latter only when the commit stages PHP. The staged set is the subset
  an IDE ticked, so a partial commit from PhpStorm checks exactly what it commits. Skip it with
  `OROBOX_SKIP_PRECOMMIT=1` or `git commit --no-verify`. An existing hook is kept as
  `pre-commit.bak` after a confirmation.
- Added an update notice: commands print a one-line hint when a newer release exists. The lookup
  is cached for 24 hours, bounded by a 3-second timeout, and skipped when stdout is not a
  terminal, so it never delays or clutters scripted runs.
- Added agent mode to the QA and PHPUnit commands, so their output can be consumed by a coding
  agent instead of a human reader.
- Added `BindsVarToHost` to the installation-type configuration, making the `var/` bind mount a
  property of the install type rather than a hard-coded choice.
- Fixed the QA config merge: project overrides stay in effect when only one half of the config
  pair is present.
- Release candidates are published as GitHub pre-releases again (`release.prerelease: auto` in
  `.goreleaser.yaml`), so `releases/latest`, `orobox self-update` and the update notice resolve to
  the newest stable tag rather than to the newest `rcN`. Every existing `rcN` and `-dev` release
  was re-flagged as a pre-release to match.
- Bumped `golang.org/x/term` to 0.46.0, `golang.org/x/sync` to 0.23.0, and the GitHub Actions used
  in CI (`checkout`, `setup-go`, `setup-python`, `upload-artifact`, `stale`, `docker/login-action`).

## [1.0.0-rc31] - 2026-09-07

- Added `dockerfile` to `.orobox.yaml`: a project-owned Dockerfile that extends the published
  image, so a project can install the system libraries, PHP extensions and tools it depends on.
  Orobox passes the published image for the configured `oro_version` in the `OROBOX_BASE_IMAGE`
  build argument and requires the Dockerfile's final stage to use it, so an Oro upgrade never
  means editing the Dockerfile. The image is rebuilt automatically when the Dockerfile, a file
  in its build context or the base image changes — no new command and no need to re-run `init`
  — and `orobox up --rebuild` forces a cache-less build. Projects that do not use the key keep
  running the published image with no local build.

## [1.0.0-rc30] - 2026-09-02

- Added a comprehensive end-to-end test suite (`e2e/`) covering bundle/project/demo installs
  across OroCommerce 5.1–7.0, running nightly and on manual dispatch.
- Added the `create` command to scaffold a new project or bundle source tree.
- Added the `ci-init` command to generate a GitLab CI pipeline from `.orobox.yaml`.
- Added support for pre-installed database seeds in runtime images, cutting install time in CI
  and locally.
- Reworked the QA toolchain: the QA tools now share a Composer vendor tree with the application
  (fixing Symfony/Twig class-redeclaration fatals), project and base tool configs are merged
  instead of one replacing the other, and Rector/Stylelint gained GitLab Code Quality report
  support.
- Switched the JS linters (ESLint, Stylelint) to run OroCommerce's own installed binaries
  instead of installing separate copies.
- Various CI diagnostics (Node/pnpm version checks), database-readiness checks, and e2e
  stability fixes.

## [1.0.0-rc29] - 2026-08-19
- Added a pre-installed OroCommerce database dump ("patch") to published runtime images.

## [1.0.0-rc28] - 2026-08-19
- Added configurable SSH agent forwarding for Composer repositories, detected from the
  repository URLs declared in `composer.json`.

## [1.0.0-rc27] - 2026-08-19
- Added WebSocket support (Oro SyncBundle) with the required environment wiring.

## [1.0.0-rc26] - 2026-08-19
- Fixed QA tools installation to handle packages the application does not declare.

## [1.0.0-rc25] - 2026-08-19
- Made the PHPStan cache environment configurable — `dev` locally, `test` in CI.

## [1.0.0-rc24] - 2026-08-18
- Added a `checks` CLI command with engine resolution and QA tool reporting.
- Added support for multiple PHPUnit test suites per deploy stage.

## [1.0.0-rc23] - 2026-08-17
- Added cached-task reporting and optimized artifact builds in the deploy pipeline.

## [1.0.0-rc22] - 2026-08-17
- Fixed the extend caches not rebuilding on every run that restores a database dump.

## [1.0.0-rc21] - 2026-08-17
- The deploy pipeline now warms the cache before running the QA/test suites.

## [1.0.0-rc20] - 2026-08-17
- PHPStan now runs with `--memory-limit=-1 --no-progress` in the pipeline.

## [1.0.0-rc19] - 2026-08-17
- Added `--ref`, `--cache-scope` and `--base-cache-scope` to `orobox deploy`.
- Added cloning over HTTPS when only a git token is available.
- Added seeding a stage's test database from a base cache scope.

## [1.0.0-rc18] - 2026-08-14
- Added deployer task tracking and release-step output streaming.
- Added the `--skip-qa` / `--skip-test` / `--skip-release` deploy flags.

## [1.0.0-rc17] - 2026-08-14
- Added the `deploy` and `deploy-init` commands (PHP Deployer integration, run through Dagger).
- Made the release symlink absolute and improved `.gitattributes` handling for `source_dir`
  deployments.

## [1.0.0-rc16] - 2026-07-29
- Added the `demo` install type.
- Improved QA tool compatibility with newer Symfony versions.

## [1.0.0-rc15] - 2026-07-02
## [1.0.0-rc14] - 2026-07-02
- Added `--remove-orphans` to the `down` command.
- Refactored SSH agent handling on Linux.

## [1.0.0-rc12] - 2026-07-02
- Improved `ssh-add` behavior for encrypted keys.

## [1.0.0-rc11] - 2026-07-01
- Added SSH agent forwarding for private Composer repositories.

## [1.0.0-rc10] - 2026-07-01
- Improved SSH and home-directory handling inside containers.

## [1.0.0-rc9] - 2026-07-01
- Fixed a missing `/etc/passwd` entry for the host UID inside containers.

## [1.0.0-rc8] - 2026-07-01
- Added `openssh-client` to the runtime image for SSH support.

## [1.0.0-rc7] - 2026-06-30
- Added the `project` install type — a repository that **is** the OroCommerce application,
  alongside the original `bundle` type.
- Added Composer auth and SSH credential forwarding into containers for private repositories.

## [1.0.0-rc6] - 2026-06-26
- Fixed a stale project `php-cs-fixer` binary being picked up before QA tools install.

## [1.0.0-rc5] - 2026-06-23
- Added `--ignore-workspace-root-check` for pnpm during QA tools installation.
- Added extra PHPStan packages to QA initialization.

## [1.0.0-rc4] - 2026-06-17
- Fixed the Xdebug hotfix for the consumer process (TTY handling).

## [1.0.0-rc3] - 2026-04-28
- Added Composer repository configuration support to bundle setup and initialization.

## [1.0.0-rc2] - 2026-04-17
- Renamed the CLI binary from `oro` to `orobox`.

## [1.0.0-rc1] - 2026-04-17
Initial public-facing release candidate, following the `0.0.1-dev` through `0.0.18-dev` internal
line. Established the shape the tool still has today:
- `init`, `up`, `down`, `shell`, `logs`, `console`, `test`, `qa-init`, `qa`, `clean`, `run`,
  `xdebug` and `self-update` commands.
- Docker Compose generation with optional services (Redis, RabbitMQ, Elasticsearch/OpenSearch,
  Mailpit, RedisInsight, Kibana, Adminer).
- The QA toolchain (PHPStan, Rector, PHP-CS-Fixer, Twig-CS-Fixer, ESLint, Stylelint) in an
  isolated `vendor-bin/qa` Composer namespace.
- `tmpfs`-backed test databases and a dedicated `db-test` service.
- Multi-version OroCommerce support and published runtime images per version.
