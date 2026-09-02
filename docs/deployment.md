[← Back to README](../README.md) · [Documentation index](README.md)

# Deployment

### 13. Deployment Initialization (`deploy-init`)
Installs PHP Deployer into the isolated `vendor-bin/deploy` namespace, asks for the deploy stages, writes them under `deploy` in `.orobox.yaml`, and generates two PHP files.

**Available for `type: project` only** — deployment ships the whole application checkout, which is what the project type is.
```bash
orobox deploy-init
```
Generated files:
- `deploy.php` — the Deployer entry point. Created only when absent, and yours afterwards: put shared files/dirs and any project-specific tasks here.
- `vendor-bin/deploy/orobox/oro.php` — the Oro recipe. Rewritten on every `deploy-init` run, so recipe fixes reach existing projects without a manual merge.

Commit `deploy.php`, `vendor-bin/deploy/orobox/oro.php`, `vendor-bin/deploy/composer.json` and `vendor-bin/deploy/composer.lock`. The recipe is committed even though `deploy-init` rewrites it: nothing in the pipeline regenerates it, and `deploy.php` requires it by path. The pipeline installs Deployer's vendor tree itself, so that does not need to be committed.

Resulting `deploy` block:
```yaml
deploy:
  pre_built_assets_enabled: false
  repository: git@gitlab.com:acme/shop.git
  # source_dir: b2b   # monorepo only: the subdirectory holding the Oro application
  stages:
    - name: staging
      ref: develop
      host: staging.acme.com
      user: deploy
      deploy_path: /var/www/oro
      test_suites: [unit, functional]
    - name: production
      ref: main
      host: acme.com
      user: deploy
      port: 2222
      deploy_path: /var/www/oro
      keep_releases: 5
      test_suites: [unit]
      restart_command: sudo systemctl restart oro-consumer
```

Re-running `deploy-init` reuses the existing values as prompt defaults, so it doubles as an edit flow.

### 14. CI Initialization (`ci-init`)
Generates the GitLab CI pipeline from `.orobox.yaml`.

**Available for `type: project` only** — the only CI-viable engine is Dagger, and its image exists
for the project type alone. Any other type falls back to the compose engine, which would need a full
`composer install` and a full `oro:install` per job.

```bash
orobox ci-init
```

Generated files:
- `.gitlab-ci-orobox.yml` — the `orobox:lint`, `orobox:test` and `orobox:deploy:<stage>` jobs.
  Rewritten on every run, so job improvements reach existing projects without a manual merge.
- `.gitlab-ci.yml` — created only when absent, and yours afterwards. It does nothing but
  `include: - local: .gitlab-ci-orobox.yml`; put your own stages, variables and jobs here.

One deploy job is generated per stage in the `deploy` block, gated on that stage's `ref` and set to
`when: manual`. Remove `when: manual` in your own file to release on every push instead.

The generated jobs install the `orobox` binary pinned to the version that generated the file, plus
`git`, which the pipeline needs to derive its upload exclude list from `.gitignore`.

If `.gitlab-ci.yml` already existed without the include, `ci-init` prints the two lines to add.

### 15. Deploy (`deploy`)
Runs the full pipeline for one stage: build and check in [Dagger](https://dagger.io), then release through PHP Deployer.
```bash
orobox deploy production
```
The stage argument may be omitted when exactly one stage is configured.

Options:
- `--yes`, `-y`: Skip the confirmation prompt (implied when there is no TTY, e.g. in CI).
- `--debug`, `-d`: Print every command's full output instead of its last lines, and stream the Dagger engine output.
- `--no-cache`: Rebuild everything the run could have reused — the dependency layers, the QA install and the test database.
- `--skip-qa`: Skip the QA checks. The QA tool set is not installed either, so nothing is paid for it.
- `--skip-test`: Skip the test suites.
- `--skip-release`: Check the code, then stop before the remote release. Nothing connects to the stage host, so the confirmation prompt is skipped and no SSH deploy credentials are needed — only whatever the clone requires.

The three `--skip-*` flags may be combined freely, including all at once. When at least one is set,
the deploy plan printed before the run names the skipped steps on a `Skipping:` line.

The release artifacts — the vendor tree and, unless the repository ships them, the assets — are
built by a run that releases, and by a run that has nothing to check either
(`--skip-qa --skip-test --skip-release`), which is how a build is asked for on its own. They are
*not* built by a run that only checks, because nothing but a release reads them and, unlike the
dependency layers, they are rebuilt from scratch at every commit: a lint job that produced them
would pay an authoritative `dump-autoload` and a tar of the whole vendor tree for a file it then
discards. The plan's `Artifacts:` line says which of the two a run is.

Every command the pipeline runs is reported as it happens: a line when it starts, then its own
output — `composer install`, `oro:assets:install`, each QA tool, `bin/simple-phpunit`, the
Deployer run — with the elapsed time and, without `--debug`, only the last lines of the output.
A command that is still running says so once a minute. QA and tests run concurrently, so each
line is prefixed with the step it belongs to (`[qa]`, `[test]`).

A command the engine serves from its cache is reported as `(0s, from cache)` and shows no output.
Its output does exist — a cached exec's stdout is part of what the cache holds, and the SDK replays
it verbatim — but it is what the command printed the last time it really ran, so printing it would
report an installation that did not happen.

What happens, in order:

1. **Build** — Dagger clones the stage's `ref` and installs the dependencies in the published `algoritmadev/orobox:<oro_version>-project-latest` image, then dumps the production autoloader and packs `vendor.tar.gz`. The install itself sees only `composer.json` and `composer.lock`, which is what makes it reusable between runs (see [What the pipeline caches](#what-the-pipeline-caches)). With `source_dir` set, only that subdirectory of the clone becomes the application root, so a monorepo builds just its Oro project.
2. **Assets** — only when `pre_built_assets_enabled: false`: runs `oro:assets:install --env=prod` and packs `public/build`, `public/js` and `public/media/js` into `assets.tar.gz`.
3. **QA and tests** — run concurrently off a shared tree that also carries the dev dependencies, PHPUnit among them, so the `--no-dev` artifact from step 1 stays dev-free. QA runs every tool enabled under `test.qa` in check-only mode (`--dry-run`, no `--fix`), because a fix inside the pipeline container would be discarded. Tests run the suites listed in the stage's `test_suites` against a Dagger-managed PostgreSQL service; `functional` first restores the cached Oro test install, or performs one when the cache no longer matches.
4. **Release** — only if everything passed. Deployer clones the same `ref` on the remote host (only `source_dir` when set, through its `sub_directory` option), uploads and extracts the tarballs, updates the application, installs the served assets, warms the cache, swaps the `current` symlink and runs the stage's `restart_command`.

The tarballs are also exported to `var/orobox/deploy/<stage>/` on the host, so a CI job can publish them as artifacts.

**Migrations.** `oro:platform:update` cannot skip migrations selectively, so the recipe compares the migration files of the new release (`src/` plus `vendor/oro/`) with the previous one. When they differ — or on a first deploy — it runs `oro:platform:update --force`. When they do not, it runs the same chain without the two migration commands, so changed cron, workflow, process, permission and translation definitions still get applied.

**Assets on the remote.** No supported Oro version accepts `--skip-assets` on `oro:platform:update`, and that command never runs webpack. The remote therefore only ever runs Symfony's `assets:install`; the built assets come either from the repository (`pre_built_assets_enabled: true`) or from `assets.tar.gz`.

#### Keeping development files out of a release

Use git's own mechanism: mark the paths `export-ignore` in `.gitattributes`.

```gitattributes
/tests           export-ignore
/.github         export-ignore
/.gitlab-ci.yml  export-ignore
/.orobox.yaml    export-ignore
/docker-compose.yml export-ignore
/phpstan.neon    export-ignore
```

Deployer checks the code out with `git archive` (`update_code_strategy` defaults to `archive`), and `git archive` honors `export-ignore`, so those files never reach the remote host at all.

Three things to know:

- The rules are read from the `.gitattributes` of the tree being deployed. A tag created before you added them still carries the files.
- Keep `update_code_strategy` on `archive`. Switching it to `clone` in `deploy.php` — which you would only do to get the `.git` directory into the release — disables `export-ignore`.
- **With `deploy.source_dir` set, the `.gitattributes` must live inside that directory.** Deployer archives the subtree (`git archive <ref>:<source_dir>`), and git only reads `.gitattributes` files that are part of the archived tree, so a repository-root `.gitattributes` is never consulted. Put the rules in `<source_dir>/.gitattributes` with paths relative to that directory:

  ```gitattributes
  /tests           export-ignore
  /.orobox.yaml    export-ignore
  ```

  A root `.gitattributes` listing `<source_dir>/tests` still works for a full-tree `git archive`, but not for the deploy.

**Failed deploys.** If any remote step fails, the recipe removes `releases/<n>`, points `current` back at the previous release, drops `.dep/deploy.lock` and rewrites Deployer's bookkeeping — so the migration comparison above always has an intact previous release to work with.

Requirements:
- A reachable Docker daemon: the Dagger SDK provisions its engine as a container.
- SSH access to the stage host, either through an agent (`eval "$(ssh-agent)" && ssh-add`) or a key in `OROBOX_DEPLOY_SSH_KEY`.
- `rsync` on the remote host, which Deployer uses to upload the artifacts. It is installed in the pipeline container automatically.

Environment variables read by `orobox deploy`:
- `OROBOX_DEPLOY_SSH_KEY`: private key for the remote host, used instead of an agent. Intended for CI.
- `OROBOX_DEPLOY_GIT_TOKEN` / `OROBOX_DEPLOY_GIT_USER`: credentials for cloning an `https` repository. `CI_JOB_TOKEN` is picked up automatically with the `gitlab-ci-token` username.

#### GitLab CI example

`orobox ci-init` generates the three-job pipeline — lint, test, deploy — see
[CI Initialization (`ci-init`)](#14-ci-initialization-ci-init). The lint and test jobs use
`orobox qa` and `orobox test` rather than a `deploy` in disguise.

Add the deploy key as a masked CI variable named `OROBOX_DEPLOY_SSH_KEY`.

In CI the pipeline builds from the job's checkout, so no clone happens and no git credentials are
needed for it; the release still checks out the built commit on the remote host. A deploy run
outside CI clones the configured `repository` instead, using the SSH agent, or `CI_JOB_TOKEN` when
the URL is `https`.

#### What the pipeline caches

The pipeline installs the dependencies from `composer.json` and `composer.lock` alone, before the
application sources are added, so Dagger reuses the whole vendor tree — production, development
and the QA tools — for as long as the lock file does not change. A commit that touches only
application code pays nothing for it.

For a stage running the `functional` suite, the Oro test install is cached as a database dump and
restored at the start of every run. The dump is rebuilt when `composer.lock` or any migration file
under `src/` changes. Restoring rather than reusing the cluster means each run starts from an
identical database.

The QA and test caches are scoped to the Oro version and the stage's git ref, so two stages on
different refs do not invalidate each other.

`orobox deploy <stage> --no-cache` rebuilds all of it.

#### Cache warmth in CI

Both the layer cache and the mounted volumes live inside the Dagger engine, not in the repository.
A GitLab job that starts a throwaway `docker:dind` service therefore begins with an empty cache
every time.

This now governs three jobs, not one: with the Dagger engine, `orobox qa` and `orobox test` depend
on the same caches. It is also the whole reason to use that engine in CI — against a cold engine,
`orobox qa` is *slower* than running the tools in a development stack, because it pays for a full
`composer install` and a full `oro:install` before analysing anything.

Two ways to keep it warm:

- give the dind service a persistent data volume in the runner configuration, so the engine
  container and its caches survive between jobs:

  ```toml
  # /etc/gitlab-runner/config.toml
  [runners.docker]
    volumes = ["/certs/client", "/var/lib/gitlab-runner/dind:/var/lib/docker"]
  ```

- or run one long-lived Dagger engine and point the jobs at it:

  ```yaml
  variables:
    _EXPERIMENTAL_DAGGER_RUNNER_HOST: tcp://dagger-engine.internal:8080
  ```
