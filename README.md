# Orobox - CLI Tool for OroCommerce Development

Orobox is a command-line tool (CLI) developed in Go to quickly set up an isolated and reproducible development environment for OroCommerce. It supports the development of individual **bundles**, whole OroCommerce **projects**, and production-tuned **demo** instances.

## ⚠️ Important Disclaimer
**WARNING: This tool is designed EXCLUSIVELY for local development. It MUST NOT be used in production environments.** Orobox configures the environment to facilitate debugging and development, which may not comply with security requirements and best practices necessary for a production environment.

## Prerequisites
Before installing Orobox, make sure you have installed on your system:
- **Docker** and **Docker Compose**
- [**mkcert**](https://github.com/FiloSottile/mkcert) (Optional, but required if you want to use **SSL** locally)

## Installation

### Linux / macOS

Run the following command in your terminal. It will automatically detect your operating system and architecture:

```bash
curl -sSfL "https://github.com/algoritma-dev/orobox/releases/download/1.0.0-rc22/orobox_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/')" -o ~/.local/bin/orobox && chmod +x ~/.local/bin/orobox && hash -r
```

*Note: Make sure `~/.local/bin` is in your `PATH`.*

### Windows (PowerShell)

Run the following command in PowerShell:

```powershell
mkdir -Force "$HOME\.local\bin"; iwr "https://github.com/algoritma-dev/orobox/releases/download/1.0.0-rc22/orobox_Windows_$($env:PROCESSOR_ARCHITECTURE.ToLower().Replace('amd64','x86_64')).exe" -OutFile "$HOME\.local\bin\orobox.exe"
```

*Note: Make sure to add `%USERPROFILE%\.local\bin` to your User `Path` environment variable.*

## Configuration (`.orobox.yaml`)

Orobox uses a configuration file called `.orobox.yaml` in the root of your bundle. If the file does not exist, the `init` command will guide you through its interactive creation.

Example `.orobox.yaml` file:
```yaml
type: bundle
class: MyBundle
namespace: MyVendor\Bundle\MyBundle
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
    ssl: false
services:
  redis: true
  redisinsight: true
  mailpit: true
  rabbitmq: true
  elasticsearch: false
  kibana: false
  adminer: true
test:
  use_tmpfs: true
  tmpfs_size: 1g
  qa:
    eslint: true
    stylelint: false
commands:
  - name: "otr"
    command: "php bin/console oro:test:run"
    description: "Runs the Shippy Pro tests suite"
    service: "application"
composer:
  # Tokens for private repositories. Mirrors Composer's COMPOSER_AUTH schema and is
  # injected only into the containers that run composer (never committed or baked
  # into long-running services).
  auth:
    github-oauth:
      github.com: ghp_yourtokenhere
    http-basic:
      repo.packagist.com:
        username: token
        password: yourtokenhere
  repositories:
    - type: vcs
      url: https://github.com/my-org/private-repo.git
    - type: composer
      url: https://repo.packagist.com/my-org/
    # SSH-format URLs are supported too; orobox forwards your host SSH agent into
    # the container automatically (start ssh-agent and `ssh-add` your key first).
    - type: vcs
      url: git@github.com:my-org/ssh-repo.git
```

### Installation types (`type`)

Orobox supports three installation types, selected via the `type` field (or the `init` prompt / `--type` flag):

- **`bundle`** (default): your repository is a single bundle that gets grafted onto a prebuilt OroCommerce application. Orobox downloads the Oro app into a named volume, mounts your bundle under `bundles/<namespace>`, wires it via `composer config repositories.bundle` + `composer require`, and syncs the resolved vendor back to the host. Requires `class` / `namespace`.
- **`project`**: your repository **is** the whole OroCommerce application. Orobox bind-mounts the checkout directly onto `/var/www/oro`, runs `composer install` (resolving dependencies from your repo's own `composer.lock`), then runs the Oro installer. No bundle `namespace`/`class` is needed, and no vendor is synced back to the host.

  > The project repository must contain a `composer.json` (and ideally a `composer.lock`). If the lock file is absent, Composer resolves from `composer.json`.

- **`demo`**: identical to `project` in how sources and vendors are wired, but the stack runs production-tuned — `ORO_ENV=prod`, OPcache enabled with `opcache.validate_timestamps=0`, and no Xdebug compiled into the image. Intended for demo and staging instances, not for development: PHP will not pick up source edits until the container restarts.

Example `project` config:
```yaml
type: project
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
    ssl: false
```

Example `demo` config:
```yaml
type: demo
oro_version: "6.1"
domains:
  - host: demo.local
    root: public
    ssl: false
```

#### Environment files per install type

- **`bundle`**: Orobox bind-mounts its generated `.env` and `.env.test` over the application's `.env-app.local` and `.env-app.test`. Orobox owns those files; edit them through `.orobox.yaml` or by placing a `.env` / `.env.test` next to `.orobox.yaml`.
- **`project` and `demo`**: the checkout owns `.env-app`, `.env-app.local` and `.env-app.test`. On the first `orobox init`, Orobox copies its generated `.env` to `.env-app.local` and `.env.test` to `.env-app.test` **only if those files do not already exist**, then never touches them again — subsequent `orobox up` / `run` / `test` invocations leave your edits alone.

  > **Upgrading an existing `project` setup:** earlier Orobox versions bind-mounted the internal env files over `.env-app.local` and `.env-app.test`, so your checkout may not contain them. Either re-run `orobox init` to have them seeded, or copy them yourself from the Orobox internal directory. If they are missing, the application starts with no database DSN.

  > The `db` and `db-test` containers still read `POSTGRES_*` from Orobox's internal `.env` and `.env.test`. If you change `ORO_DB_*` in your own `.env-app.local`, mirror it there, or the application and the database will disagree on credentials.

### Configuration Fields
- `type`: Installation type — `bundle` (default), `project` or `demo`. See [Installation types](#installation-types-type).
- `class`: Name of the bundle class (bundle type only).
- `namespace`: PHP namespace of the bundle (bundle type only).
- `oro_version`: OroCommerce version (e.g., "7.0", "6.1", "6.0", "5.1").
- `domains`: List of domains for the environment.
- `services`: Configuration for optional services and tools:
    - `redis`: (bool) Enable/disable Redis.
    - `redisinsight`: (bool) Enable/disable RedisInsight.
    - `mailpit`: (bool) Enable/disable Mailpit.
    - `rabbitmq`: (bool) Enable/disable RabbitMQ.
    - `elasticsearch`: (bool) Enable/disable Elasticsearch/OpenSearch.
    - `kibana`: (bool) Enable/disable Kibana (only if Elasticsearch is enabled).
    - `adminer`: (bool) Enable/disable Adminer (PostgreSQL manager).
- `test`:
    - `use_tmpfs`: (bool) If enabled, uses RAM (tmpfs) for database files in the test container, significantly improving performance but data is lost on container restart.
    - `tmpfs_size`: (string) Size of the tmpfs mount (e.g., "1g", "512m").
    - `qa`: (map) Enable or disable individual QA tools. Any tool not listed defaults to enabled. Useful when a bundle has no JavaScript (disable `eslint`, `stylelint`) or no Twig templates (disable `twig_cs_fixer`).
        - `phpstan`: (bool) Enable/disable PHPStan.
        - `rector`: (bool) Enable/disable Rector.
        - `php_cs_fixer`: (bool) Enable/disable PHP-CS-Fixer.
        - `twig_cs_fixer`: (bool) Enable/disable Twig-CS-Fixer.
        - `eslint`: (bool) Enable/disable ESLint.
        - `stylelint`: (bool) Enable/disable Stylelint (SCSS/LESS/SASS/CSS).
- `commands`: (list) List of custom commands that can be run in the container:
    - `name`: (string) Name of the command (e.g., `otr`).
    - `command`: (string) The actual command to execute (e.g., `php bin/console oro:test:run`).
    - `description`: (string) Description of the command (displayed in help).
    - `service`: (string, optional) Default service to run the command in (e.g., `application`).
- `composer`: (map) Composer-specific settings for the bundle.
    - `repositories`: (list) Additional Composer repositories to register in the OroCommerce project during installation. Accepts the same format as Composer's [`repositories`](https://getcomposer.org/doc/05-repositories.md) field (VCS, Composer, path, package, etc.). These are merged with any existing repositories in the project's `composer.json`. Required when the bundle depends on packages hosted in private repositories.
    - `auth`: (map) Credentials for private repositories, using Composer's [`COMPOSER_AUTH`](https://getcomposer.org/doc/03-cli.md#composer-auth) schema (`github-oauth`, `gitlab-token`, `http-basic`, `bearer`, ...). Serialized to JSON and injected as the `COMPOSER_AUTH` environment variable only into the containers that run composer, so tokens are never committed or baked into long-running services.
    - **SSH repositories**: when any `repositories[].url` uses an SSH transport (`git@host:org/repo.git` or `ssh://...`), orobox forwards your host SSH agent into the composer container (`SSH_AUTH_SOCK` is bind-mounted; on Docker Desktop the built-in agent socket is used, on Linux your live `$SSH_AUTH_SOCK`). Ensure an agent is running with your key loaded (`eval "$(ssh-agent)" && ssh-add`).
- `deploy`: (map, `project` type only) Deployment configuration, generated by [`deploy-init`](#12-deployment-initialization-deploy-init). This is the single source of truth: `deploy.php` reads host, user, port, path, repository and ref from the `OROBOX_DEPLOY_*` variables Orobox injects, so the two files cannot drift.
    - `pre_built_assets_enabled`: (bool) `true` when the repository already ships built assets — the pipeline then has no assets stage. `false` makes the pipeline run `oro:assets:install --env=prod` and upload the result as `assets.tar.gz`. Either way the remote never rebuilds them.
    - `repository`: (string, optional) URL Deployer clones on the remote host. Defaults to `git remote get-url origin`.
    - `source_dir`: (string, optional) Repository-relative directory holding the OroCommerce application. Leave empty when the repository root **is** the application. Set it for a monorepo that keeps the application beside other projects (e.g. `b2b` in a repository with `b2b/` and `api_client/`): the pipeline builds from that directory and Deployer's `sub_directory` extracts only it into the release, so the release looks like a plain Oro checkout and the sibling projects never reach the remote host. Must be relative and inside the repository.
    - `stages`: (list) One entry per deploy target.
        - `name`: (string) Stage selector, e.g. `orobox deploy production`.
        - `ref`: (string) Git ref built by the pipeline **and** checked out on the remote, so the artifacts always match the deployed code.
        - `host`, `user`, `port`: (string/string/int) SSH target. `port` defaults to `22`.
        - `deploy_path`: (string) Deployer's `deploy_path` on the remote host.
        - `keep_releases`: (int, optional) Releases kept on the remote. Defaults to `5`.
        - `test_suites`: (list, optional) PHPUnit suites the pipeline runs: `unit`, `functional` or both. Defaults to `[unit]`; `functional` adds a full `oro:install --env=test` and is much slower.
        - `restart_command`: (string, optional) Command run on the remote after the update, for consumers and cron.

*Note: Versions of PHP, PostgreSQL, Node.js, and other components are automatically determined by the `oro_version` setting and cannot be changed manually.*

### Global Flags
These options can be used with any command:
- `--config`: Specifies an alternative configuration file (default: `.orobox.yaml`).

## Domains and SSL
- **Hosts File**: For each domain defined in `.orobox.yaml`, you must add an entry to your system's `hosts` file (e.g., `/etc/hosts` on Linux/macOS or `C:\Windows\System32\drivers\etc\hosts` on Windows) pointing to `127.0.0.1`.
  Example: `127.0.0.1 oro.demo`
- **SSL with mkcert**: If you enable SSL, Orobox uses `mkcert` to generate local certificates. Ensure `mkcert` is installed and you have run `mkcert -install` once on your machine.

## Command Usage

The main command is `oro` (or `orobox`, depending on how you installed it).

### 1. Initialization (`init`)
Prepares the development environment in your bundle repository.
```bash
orobox init
```
This command:
- Creates the `.orobox.yaml` file if missing (interactive mode).
- Generates SSL certificates if required.
- Configures the necessary Docker files.

Options:
- `--bundle-path`, `-b`: Bundle path (default ".").
- `--oro-version`, `-v`: OroCommerce version to use (default "6.1").
- `--bundle-namespace`, `-n`: Bundle namespace (e.g., "MyVendor/Bundle/MyBundle").
- `--type`, `-t`: Installation type — `bundle`, `project` or `demo`. If omitted, `init` prompts interactively (default `bundle`).

### 2. Start Environment (`up`)
Starts Docker containers and configures OroCommerce.
```bash
orobox up
```
The command dynamically generates the `docker-compose.yml` file, starts the services, and proceeds with the environment installation or update.

### 3. Stop Environment (`down`)
Shuts down the Docker services associated with the bundle.
```bash
orobox down
```

### 4. Shell Access (`shell`)
Accesses a container in interactive mode (default: php).
```bash
orobox shell
```

### 5. View Logs (`logs`)
Displays logs from different services in the development environment. At least one flag must be specified.
```bash
orobox logs --app
```
Options:
- `--nginx`: Nginx logs.
- `--php`: PHP-FPM logs.
- `--app`: Symfony/OroCommerce logs.
- `--consumer`: Consumer logs.
- `--cron`: Cron logs.
- `--ws`: WebSocket logs.

### 6. Symfony Console (`console`)
Executes Symfony commands in the application container.
```bash
orobox console cache:clear
```

### 7. Run Tests (`test`)
Runs PHPUnit tests within the configured environment.
```bash
orobox test
```

### 8. QA Tools Initialization (`qa-init`)
Configures and installs the necessary QA tools (PHPStan, coding standards, ESLint, Stylelint) in your bundle.
```bash
orobox qa-init
```

### 9. Run QA Tools (`qa`)
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

CLI flags always override the configuration. Example:
```bash
orobox qa --phpstan --eslint
```

PHPStan reads the dumped `test` debug container, so it needs an installed test database. Run
`orobox test-init` once before the first PHPStan run; afterwards the warmed `var/cache/test` is
reused.

### 10. Total Cleanup (`clean`)
Removes all associated containers and volumes to start from scratch.
```bash
orobox clean
```

### 11. Run Custom Commands (`run`)
Runs a custom command defined in your `.orobox.yaml` file.
```bash
orobox run <command-name>
```
Example:
```bash
orobox run otr
```
Options:
- `--service`, `-s`: Specify a custom service to run the command in (e.g., `application`).
- `--test`, `-t`: Quick flag to run the command in the `application` service with test environment override.

If you run `orobox run --help`, you will see a dynamic list of all commands configured in your `.orobox.yaml`.

### 12. Deployment Initialization (`deploy-init`)
Installs PHP Deployer into the isolated `vendor-bin/deploy` namespace, asks for the deploy stages, writes them under `deploy` in `.orobox.yaml`, and generates two PHP files.

**Available for `type: project` only** — deployment ships the whole application checkout, which is what the project type is.
```bash
orobox deploy-init
```
Generated files:
- `deploy.php` — the Deployer entry point. Created only when absent, and yours afterwards: put shared files/dirs and any project-specific tasks here.
- `vendor-bin/deploy/orobox/oro.php` — the Oro recipe. Rewritten on every `deploy-init` run, so recipe fixes reach existing projects without a manual merge.

Commit `deploy.php`, `vendor-bin/deploy/composer.json` and `vendor-bin/deploy/composer.lock`. The pipeline installs Deployer's vendor tree itself, so it does not need to be committed.

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

### 13. Deploy (`deploy`)
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
- `--skip-release`: Build, check and export the artifacts, then stop before the remote release. Nothing connects to the stage host, so the confirmation prompt is skipped and no SSH deploy credentials are needed — only whatever the clone requires.

The three `--skip-*` flags may be combined freely, including all at once: the vendor tree and the
assets are always built and always exported, so even a run that skips everything else produces the
artifacts a later release would upload. When at least one is set, the deploy plan printed before the
run names the skipped steps on a `Skipping:` line.

Every command the pipeline runs is reported as it happens: a line when it starts, then its own
output — `composer install`, `oro:assets:install`, each QA tool, `bin/simple-phpunit`, the
Deployer run — with the elapsed time and, without `--debug`, only the last lines of the output.
A command that is still running says so once a minute. QA and tests run concurrently, so each
line is prefixed with the step it belongs to (`[qa]`, `[test]`).

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

```yaml
deploy:production:
  image: docker:27
  services:
    - docker:27-dind
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_TLS_CERTDIR: ""
  script:
    - orobox deploy production --yes
  artifacts:
    paths:
      - var/orobox/deploy/production/
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
```

Add the deploy key as a masked CI variable named `OROBOX_DEPLOY_SSH_KEY`. Cloning uses `CI_JOB_TOKEN` when the configured `repository` is an `https` URL.

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
every time. Two ways to keep it warm:

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

## Debugging with Xdebug

Orobox includes Xdebug preinstalled, but disabled by default to maintain performance.


### 1. Enabling/Disabling Xdebug

You can enable or disable Xdebug in currently running containers using the `xdebug` command:

```bash
orobox xdebug enable
```

This command will:
- Apply the change immediately to running containers ("hot-patching").

To disable Xdebug:
```bash
orobox xdebug disable
```

You can specify which environment to target:
```bash
orobox xdebug enable --dev   # Development environment (default)
orobox xdebug enable --test  # Test environment
```

*Note: Since the configuration is not persistent, Xdebug will be disabled again after a container restart or `orobox up`.*

### 2. Xdebug for CLI, Consumer, and Cron
For debugging background processes or to have Xdebug enabled by default, you can set these variables in your `.env` file:
- `ORO_CONSUMER_XDEBUG_ENABLED=true`: For Message Queue consumers.
- `ORO_CRON_XDEBUG_ENABLED=true`: For Cron jobs.

After updating the `.env` file, restart the environment with `orobox up`.

### 3. PHPStorm Configuration
To debug with PHPStorm:
1.  **Listen for Debug Connections**: Ensure the 'Phone' icon (Start Listening for PHP Debug Connections) is ON.
2.  **Server Configuration**: Go to `Settings -> PHP -> Servers` and add a new server:
    - **Host**: Your domain (default: `oro.demo` or your custom domain in `.orobox.yaml`).
    - **Port**: `80` (or `443` if using SSL).
    - **Debugger**: Xdebug.
3.  **Path Mappings**: Enable "Use path mappings" and configure:
    - **Local Path**: The root folder of your bundle on the host machine.
    - **Remote Path**: `/var/www/oro/src/<BundleNamespace>` (e.g., `/var/www/oro/src/MyVendor/Bundle/MyBundle`).
4.  **Xdebug Port**: Ensure the port in `Settings -> PHP -> Debug` is set to `9003`.

## Internal Structure
The Orobox environment typically includes:
- **Nginx**: Web server configured for OroCommerce.
- **PHP-FPM / PHP-CLI**: Runtime for the application and Symfony commands.
- **PostgreSQL**: Main database.
- **Redis**: For cache and sessions (optional).
- **RabbitMQ**: Message broker (optional).
- **Elasticsearch/OpenSearch**: For search features (optional).
- **Mailpit**: To capture emails sent during development (optional).

## Development

If you want to contribute to Orobox, you can use the provided `Makefile` to simplify common tasks.

### Prerequisites
- **Go** (version 1.26 or later)
- **golangci-lint** (recommended version 1.64.0 or later)

### Available Commands
- **Linting**:
  ```bash
  make lint
  ```
- **Running Tests**:
  ```bash
  make test
  ```
- **Local Build**:
  ```bash
  make build
  ```
- **Update Version**:
  ```bash
  make set-version v=X.Y.Z
  ```

---

Developed to simplify the work of OroCommerce developers.

---

### Inspired by
- [MageBox](https://magebox.dev/)
