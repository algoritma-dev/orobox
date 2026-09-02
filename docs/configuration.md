[← Back to README](../README.md) · [Documentation index](README.md)

# Configuration

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
  # Forward the host SSH agent into the containers. Omit to auto-detect (any SSH-transport
  # URL in the repositories below, or in the project's own composer.json); true forwards
  # unconditionally, false never forwards.
  ssh_agent: true
  repositories:
    - type: vcs
      url: https://github.com/my-org/private-repo.git
    - type: composer
      url: https://repo.packagist.com/my-org/
    # SSH-format URLs are supported too; orobox forwards your host SSH agent into
    # the containers automatically (start ssh-agent and `ssh-add` your key first).
    - type: vcs
      url: git@github.com:my-org/ssh-repo.git
```

### Installation types (`type`)

Orobox supports three installation types, selected via the `type` field (or the `init` prompt / `--type` flag):

- **`bundle`** (default): your repository is a single bundle that gets grafted onto a prebuilt OroCommerce application. Orobox downloads the Oro app into a named volume, mounts your bundle under `bundles/<namespace>`, wires it via `composer config repositories.bundle` + `composer require`, and syncs the resolved vendor back to the host. Requires `class` / `namespace`.
- **`project`**: your repository **is** the whole OroCommerce application. Orobox bind-mounts the checkout directly onto `/var/www/oro`, runs `composer install` (resolving dependencies from your repo's own `composer.lock`), then runs the Oro installer. No bundle `namespace`/`class` is needed, and no vendor is synced back to the host.

  > The project repository must contain a `composer.json` (and ideally a `composer.lock`). If the lock file is absent, Composer resolves from `composer.json`.

  > Private dependencies: declare the repositories in the application's own `composer.json` as usual. Orobox reads that file to decide whether to forward your SSH agent, and `composer.ssh_agent: true` in `.orobox.yaml` forces forwarding when the SSH access is not visible there. Tokens still go in `composer.auth`.

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
- `composer`: (map) Composer-specific settings.
    - `repositories`: (list, `bundle` type only) Additional Composer repositories to register in the OroCommerce project during installation. Accepts the same format as Composer's [`repositories`](https://getcomposer.org/doc/05-repositories.md) field (VCS, Composer, path, package, etc.). These are merged with any existing repositories in the project's `composer.json`. Required when the bundle depends on packages hosted in private repositories. `project` and `demo` installs declare their repositories in the application's own `composer.json` and ignore this key.
    - `auth`: (map) Credentials for private repositories, using Composer's [`COMPOSER_AUTH`](https://getcomposer.org/doc/03-cli.md#composer-auth) schema (`github-oauth`, `gitlab-token`, `http-basic`, `bearer`, ...). Serialized to JSON and injected as the `COMPOSER_AUTH` environment variable only into the containers that run composer, so tokens are never committed or baked into long-running services.
    - `ssh_agent`: (bool, optional) Forces SSH agent forwarding on or off. When omitted, Orobox auto-detects it: forwarding is on when an SSH-transport URL appears in `repositories` above **or** in the checkout's own `composer.json`. Set it to `true` for a `project` whose private dependencies reach SSH through something Composer never sees (a git submodule, a `path` repository that is itself a checkout), and to `false` to opt out entirely.
    - **SSH repositories**: when forwarding is on — see `ssh_agent` above — Orobox bind-mounts your host SSH agent socket into the containers and sets `SSH_AUTH_SOCK` (on Docker Desktop the built-in agent socket is used, on Linux your live `$SSH_AUTH_SOCK`). This covers both the one-shot composer/git commands `orobox init` runs and the running `application` container, so `orobox shell` followed by `composer update` or `git fetch` authenticates too. `orobox init` will start an agent and load your default key if none is running; every other command uses only an agent that is already running, so start one first with `eval "$(ssh-agent)" && ssh-add`.
- `deploy`: (map, `project` type only) Deployment configuration, generated by [`deploy-init`](deployment.md#13-deployment-initialization-deploy-init). This is the single source of truth: `deploy.php` reads host, user, port, path, repository and ref from the `OROBOX_DEPLOY_*` variables Orobox injects, so the two files cannot drift.
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
