# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versioning follows
[SemVer](https://semver.org/). See [CONTRIBUTING.md](CONTRIBUTING.md#release-policy) for what
"release candidate" means for this project.

Entries below `1.0.0-rc1` are reconstructed and aggregated from `git log` for readability; they
group commits by theme rather than listing every commit. There is no stable `1.0.0` yet.

## [Unreleased]

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
