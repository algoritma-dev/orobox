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
curl -sSfL "https://github.com/algoritma-dev/orobox/releases/download/1.0.0-rc29/orobox_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/')" -o ~/.local/bin/orobox && chmod +x ~/.local/bin/orobox && hash -r
```

*Note: Make sure `~/.local/bin` is in your `PATH`.*

### Windows (PowerShell)

Run the following command in PowerShell:

```powershell
mkdir -Force "$HOME\.local\bin"; iwr "https://github.com/algoritma-dev/orobox/releases/download/1.0.0-rc29/orobox_Windows_$($env:PROCESSOR_ARCHITECTURE.ToLower().Replace('amd64','x86_64')).exe" -OutFile "$HOME\.local\bin\orobox.exe"
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

## WebSocket

The Oro websocket server (`gos:websocket:server`) runs in the `ws` service and is never
published on the host: nginx proxies the `/ws` location of every configured domain to it and
performs the HTTP upgrade, so the browser connects to the same origin it is already browsing
(`wss://` on an SSL domain, `ws://` otherwise).

Orobox owns the addressing and injects it into `php-fpm-app`, `ws`, `consumer` and `cron`, so
a project checkout carrying its own websocket settings in `.env-app.local` cannot point them
at a host that does not exist inside the Compose network:

- `ORO_WEBSOCKET_SERVER_DSN=//0.0.0.0:8080` — what the server binds to.
- `ORO_WEBSOCKET_BACKEND_DSN=tcp://ws:8080` — how PHP reaches the server, direct, without nginx.
- `ORO_WEBSOCKET_FRONTEND_DSN=//*:<published nginx port>/ws` — what the browser is told to use.

The frontend port is the published nginx port (`nginx_https_port` when at least one domain uses
SSL, `nginx_http_port` otherwise). Only one port can be advertised, so a configuration that mixes
SSL and plain domains advertises the HTTPS one and the plain domains fall back to no live updates.

Websocket logs come from `orobox logs --ws`. The connection is not required for the application
to work: when the server is down, Oro simply stops rendering the client-side sync code.

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
Options:
- `-f, --filter`: Filter tests by name.
- `-t, --testsuite`: Run a specific test suite; repeat the flag for several.
- `--engine=compose|dagger`: Where the tests run. See [Running the checks in CI](#running-the-checks-in-ci).
- `--report=gitlab`: Write a GitLab JUnit report.
- `--report-path`: Where to write it (default `var/orobox/reports/junit.xml`).
- `--cache-scope`, `--base-cache-scope`: Dagger engine only; same meaning as on `orobox deploy`.

### 8. QA Tools Initialization (`qa-init`)
Configures and installs the necessary QA tools (PHPStan, coding standards, ESLint, Stylelint) in your bundle.
```bash
orobox qa-init
```

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
application's. Every package both trees need — `symfony/console`, the Symfony contracts, `psr/log`
and the rest — is installed **once**, in the application's tree, and reached from the QA tree
through the bootstrap Orobox writes to `vendor-bin/qa/orobox/oro-autoload.php`.

That is not a size optimization. A dumped Symfony debug container inline-requires vendor files by
absolute path, and `include_once` dedupes on the path, not on the class name. With two copies
installed, PHPStan boots the kernel, the container requires the application's copy of a class the
QA copy already declared, and the run dies with:

```
Fatal error: Cannot redeclare interface Symfony\Contracts\Service\ResetInterface
```

Identical versions in both trees do not help — the paths still differ. So `orobox qa-init` (and the
deploy pipeline's QA stage) patches `vendor-bin/qa/composer.json` before Composer populates it:
each shared package the application ships is written into `replace` at the version actually
installed there and removed from the QA requirements. A package the application does **not** ship
keeps its pinned requirement and is installed in the QA tree as before.

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

| File | How it merges |
| --- | --- |
| `phpstan.neon` | NEON `includes`: base first, yours last. Relative paths in either file keep resolving against that file. |
| `rector.php` | Both configs are applied to the same `RectorConfig`, base first. Either shape works — a `static function (RectorConfig $c)` or a `RectorConfig::configure()` builder. |
| `.php-cs-fixer.dist.php` | Rules merge key by key, yours winning. Risky rules are allowed when either side allows them. Finder, cache file and indentation come from your config. |
| `.twig-cs-fixer.php` | Rulesets merge rule by rule (keyed by rule class), yours winning. Finder and the remaining `Config` settings come from your config. |
| `.eslintrc.yml`, `.stylelintrc.yml`, `.stylelintrc-css.yml` | `extends`: base first, yours last. |
| `.eslintignore`, `.stylelintignore`, `.stylelintignore-css` | **Not merged** — yours replaces the base one. Ignore patterns resolve against the directory of the file holding them, so a merged copy would re-anchor every inherited pattern. |

The merged file is generated into `vendor-bin/qa/merged/` on every run; nothing is written to your
checkout. When you ship no config of your own, the base one is used directly.

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

`--report=gitlab` makes every tool emit its own GitLab report; Orobox merges them into one
document. Nothing is converted: each tool speaks the format natively, and the two JS tools do so
through the `eslint-formatter-gitlab` and `stylelint-formatter-gitlab` packages `orobox qa-init`
installs.

With `--report`, the QA tools no longer stop at the first failure — a Code Quality report listing
only PHPStan's findings because Rector never ran would be worse than none. Every tool runs, and the
command still exits non-zero if any of them found something.

The untouched per-tool files are kept under `var/orobox/reports/raw/`. When a merged report looks
wrong, they are what says whether the tool or the merge is to blame.

#### GitLab CI

`orobox ci-init` generates the pipeline — see
[CI Initialization (`ci-init`)](#13-ci-initialization-ci-init). The generated jobs run
`orobox qa --report=gitlab`, `orobox test --report=gitlab` and `orobox deploy <stage>`, and publish
the reports at the paths those commands write to.

`--skip-qa --skip-test` on the generated deploy job is sound in a way it would not be otherwise:
the lint and test jobs ran the same code, on the same commit, against the same cache scope.

Note that `orobox deploy` in CI also builds from `CI_PROJECT_DIR` rather than cloning, and the
release therefore checks out the commit that was built (`CI_COMMIT_SHA`) rather than the stage's
configured `ref`. An explicit `--ref` still wins. This is what keeps the deployed revision equal to
the tested one.

All of this depends on the Dagger engine's cache surviving between jobs — see
[Cache warmth in CI](#cache-warmth-in-ci).

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

### 13. CI Initialization (`ci-init`)
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

### 14. Deploy (`deploy`)
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
[CI Initialization (`ci-init`)](#13-ci-initialization-ci-init). The lint and test jobs use
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
