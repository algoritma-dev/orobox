[← Back to README](../README.md) · [Documentation index](README.md)

# Command Usage

The main command is `oro` (or `orobox`, depending on how you installed it).

QA (`qa-init`, `qa` — sections 9 and 10) and deployment (`deploy-init`, `ci-init`, `deploy` —
sections 13 to 15) have their own pages: [QA tools](qa.md) and [Deployment](deployment.md).
`xdebug` is described in [Debugging](debugging.md). How the commands below fit together to
customize the stack is the subject of [Customizing the environment](customization.md).

### 1. Scaffolding (`create`)
Creates a new source tree on disk and stops. It does **not** touch Docker, the
configuration, or the OroCommerce install — that is `init`'s job. The typical flow is
`create` → `cd` into the new directory → `init`.

Scaffold a project checkout:
```bash
orobox create project my-project
```
This clones the public `oroinc/orocommerce-application` skeleton into `my-project/`
(no credentials needed), then removes its `.git` so you start with a fresh history.

Project options:
- `--oro-version`, `-v`: OroCommerce version to scaffold (default "6.1").

Scaffold a bundle skeleton:
```bash
cd my-project
orobox create bundle 'Acme\Bundle\FooBundle'
```
The argument is the bundle **namespace**, and where the bundle lands is read from the
`composer.json` in the current directory. An OroCommerce application autoloads
`"": "src/"`, so inside a project checkout the command above writes
`src/Acme/Bundle/FooBundle/`. The project already autoloads that path and Oro's kernel
discovers the generated `Resources/config/oro/bundles.yml`, so the bundle needs no
`composer.json` of its own — clear the cache and it is loaded:

```text
src/Acme/Bundle/FooBundle/
├── AcmeFooBundle.php
├── DependencyInjection/
│   ├── AcmeFooExtension.php
│   └── Configuration.php
└── Resources/config/
    ├── services.yml
    └── oro/bundles.yml
```

Run outside a PHP project — or with `--standalone` — and the bundle becomes its own
composer package instead, in a directory named after its class, with a `composer.json`
declaring its PSR-4 prefix and a `.gitignore`. That is the shape `type: bundle` expects,
so it is where you start a bundle you intend to publish:

```bash
orobox create bundle 'Acme\Bundle\FooBundle'   # in an empty directory
cd AcmeFooBundle && orobox init
```

The class name is derived the way Oro derives its own — the vendor segment joined to the
bundle segment, so `Acme\Bundle\FooBundle` gives `AcmeFooBundle` exactly as
`Oro\Bundle\UserBundle` gives `OroUserBundle` — and the DI alias is its snake_case form
(`acme_foo`). A fully-qualified class (`Acme\Bundle\FooBundle\AcmeFooBundle`) is
accepted too when you want to name it yourself.

Bundle options:
- `--path`: Target directory. Overrides the PSR-4 placement; relative to the current
  directory unless absolute. Changes the location, not the shape.
- `--standalone`: Generate the bundle as its own composer package, ignoring the current
  directory's PSR-4 map.
- `--class`: Bundle class name (default: derived from the namespace).
- `--package`, `-p`: Composer package name for a standalone bundle (default: derived from
  the namespace, e.g. `acme/foo-bundle`).

No network access is required for `create bundle`. Both `create` subcommands refuse to
write into a directory that already exists and is non-empty.

### 2. Initialization (`init`)
Provisions the development environment in the **current directory** (run it inside a
directory produced by `create`, or an existing project/bundle checkout).
```bash
orobox init
```
This command:
- Creates the `.orobox.yaml` file if missing (interactive mode).
- Generates SSL certificates if required.
- Configures the necessary Docker files.
- Runs the OroCommerce install.

Options:
- `--oro-version`, `-v`: OroCommerce version to use (default "6.1").
- `--bundle-namespace`, `-n`: Bundle namespace (e.g., "MyVendor/Bundle/MyBundle").
- `--type`, `-t`: Installation type — `bundle`, `project` or `demo`. If omitted, `init` prompts interactively (default `bundle`).

#### Pre-installed database

Published Orobox images carry a dump of an OroCommerce already installed for their own version.
Instead of running `oro:install`, `init` restores that dump and reconciles the schema with
`oro:platform:update`, which reaches the same database in roughly a third of the time. The same
dump is the last resort of the QA cache and of the functional test database, so a CI runner — where
every cache starts empty — no longer pays for a full install on every run.

The dump holds no sample data. When `ORO_SAMPLE_DATA=y` (the default in `.env`) the demo fixtures
are loaded on top of the restored database.

The dump is only used when it can be reconciled with the project it is restored into:

- the install must ask for the administrator, organization and locale the image was built with;
- the project's resolved `oro/platform` must not be older than the one the dump was taken from,
  because migrations do not roll back.

Anything else gets its own `oro:install` — as does an image built without a seed, a PostgreSQL
major that did not write the dump, and any restore that does not complete. Set `ORO_NO_SEED=1` to
skip the dump and always install from scratch.

### 3. Start Environment (`up`)
Starts Docker containers and configures OroCommerce.
```bash
orobox up
```
The command regenerates the compose files, the env files and the nginx configuration from
`.orobox.yaml`, builds the project's [image layer](configuration.md#image-customization-image)
when one is configured and stale, and starts the services. Run it again after changing
`.orobox.yaml`, `.env`, `php_ini` or a [compose override](configuration.md#extending-the-stack-oroboxcomposeyaml):
Compose recreates only the containers whose definition changed. When an override defines a
service with a `build:` section, `up` passes `--build` so it is rebuilt.

Flags:
- `-c`, `--clean`: tear the environment down, volumes included, before starting.
- `--rebuild`: pull the base image, then rebuild the project's image layer from its [`image` settings](configuration.md#image-customization-image) ignoring the Docker cache. Only useful with a custom layer configured: rebuilds happen automatically when the rendered Dockerfile, its build context or the base image change, so this is for the case Docker cannot see — an unpinned `RUN apk add` that should pick up a newer package.

Once the stack is up, `up` prints the application URLs, the database connection for your IDE
(host port `ports.db`) and, for every optional service compose actually runs, its URL and the DSN
to put in your `.env`, all with the host ports configured in [`ports`](configuration.md#host-ports-ports).
Adminer runs unless `services.adminer: false`; RedisInsight runs with Redis and Kibana with
Elasticsearch unless they are switched off. A URL whose port is `0` is not printed (the DSN hint,
which works inside the stack, still is). It then lists the services your [compose override](configuration.md#extending-the-stack-oroboxcomposeyaml) advertises with the `dev.orobox.url` label, after the built-in blocks:
```
Project services:
  - minio: http://localhost:9001
```
The block is omitted when no service carries the label.

### 4. Stop Environment (`down`)
Shuts down the Docker services associated with the bundle.
```bash
orobox down
```

### 5. Shell Access (`shell`)
Opens an interactive shell in a running container, `application` by default: `bash`, or `sh` when
the image has no bash. Any running service of the stack can be named, including the ones added by
a compose override (`db-test` belongs to the test stack and is reachable from `orobox test`
commands only).
```bash
orobox shell
orobox shell consumer
```

### 6. View Logs (`logs`)
Displays logs from different services in the development environment. At least one flag or service name must be specified.
```bash
orobox logs --app
orobox logs minio
orobox logs --nginx minio
```
Any compose service can be named directly, including the ones added in `.orobox.compose.yaml`. Services given by flag come first, then the named ones; a service mentioned twice is followed once.

Options:
- `--nginx`: Nginx logs.
- `--php`: PHP-FPM logs.
- `--app`: Symfony/OroCommerce logs.
- `--consumer`: Consumer logs.
- `--cron`: Cron logs.
- `--ws`: WebSocket logs.

### 7. Symfony Console (`console`)
Executes Symfony commands in the application container.
```bash
orobox console cache:clear
```

### 8. Run Tests (`test`)
Runs PHPUnit tests within the configured environment.
```bash
orobox test
```
Options:
- `-f, --filter`: Filter tests by name.
- `-t, --testsuite`: Run a specific test suite; repeat the flag for several.
- `--engine=compose|dagger`: Where the tests run. See [Running the checks in CI](qa.md#running-the-checks-in-ci).
- `--report=gitlab`: Write a GitLab JUnit report.
- `--report-path`: Where to write it (default `var/orobox/reports/junit.xml`).
- `--cache-scope`, `--base-cache-scope`: Dagger engine only; same meaning as on `orobox deploy`.

### 11. Total Cleanup (`clear`)
Removes all associated containers and volumes to start from scratch. `orobox clean` is accepted as
an alias. The named volumes declared in
a compose override are removed too; see [Extending the stack](configuration.md#extending-the-stack-oroboxcomposeyaml)
for keeping data across a cleanup.
```bash
orobox clear
```

### 12. Run Custom Commands (`run`)
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

### 16. Extending the environment (`extend`)
Writes the files that customize the stack, with the required headers and commented examples, so you do not have to remember the file names or the syntax.
```bash
orobox extend image             # docker/image/Dockerfile + image.dockerfile in .orobox.yaml
orobox extend compose           # .orobox.compose.yaml
orobox extend compose --local   # .orobox.compose.local.yaml (+ .gitignore entry if one exists)
orobox extend add               # list the recipes
orobox extend add varnish sftp  # add ready-made services
```

| Command | Effect |
| --- | --- |
| `extend image` | Creates `docker/image/Dockerfile` with the `ARG OROBOX_BASE_IMAGE` / `FROM ${OROBOX_BASE_IMAGE}` header and commented examples, and sets `image.dockerfile` in `.orobox.yaml`. The Dockerfile gets a directory of its own because that directory is the build context, hashed on every container start. A file already at that path is adopted only if it is an Orobox layer (final `FROM ${OROBOX_BASE_IMAGE}`); otherwise the command refuses and nothing is written. |
| `extend compose` | Creates `.orobox.compose.yaml` with commented examples: a new service with the `dev.orobox.url` label, an environment variable on a core service, an extra mount, and `!override` on `ports`. |
| `extend compose --local` | Creates `.orobox.compose.local.yaml` for one developer's own tweaks, and appends `/.orobox.compose.local.yaml` to `.gitignore` when that file exists and does not list it yet. |
| `extend add` | Lists the available recipes, one `name — description` line each. |
| `extend add <recipe>...` | Adds each recipe in order, see [Recipes](#recipes) below. `--force` replaces the recipe's services when `.orobox.compose.yaml` already defines them. |

What the files do is described in [Custom Dockerfile](configuration.md#custom-dockerfile-imagedockerfile) and [Extending the stack](configuration.md#extending-the-stack-oroboxcomposeyaml).

Rules shared by every `extend` subcommand:
- **It needs the project's config.** Every subcommand works against `.orobox.yaml` (or the file given with `--config`) and refuses to run without it: run [`orobox init`](#2-initialization-init) first. All files are written next to that config file, and never through a symlink pointing outside the project.
- **Nothing you wrote is replaced.** `image` and `compose` never overwrite a file: an existing one is left alone and reported as `skipped`. `add` edits `.orobox.compose.yaml`, `.orobox.yaml` and `.env` in place, only adding what they lack (`--force` replaces the recipe's own services).
- **`.orobox.yaml` keeps its comments.** The file is rewritten in place, so comments stay, but formatting is normalized: blank lines are dropped and indentation and the spacing before inline comments are tidied. Review the diff before committing. A file with several YAML documents (`---`) is refused rather than edited. If the config already names a Dockerfile (`image.dockerfile`, or the deprecated top-level `dockerfile`), that path is used instead of `docker/image/Dockerfile` and the config is left as it is; a missing file is created at the configured path.
- **One line per file**, `<action> <path>` with the path relative to the project and the action `created`, `updated` or `skipped`:
  ```
  created docker/image/Dockerfile
  updated .orobox.yaml
  ```
  With `--agent` these lines are all that stdout carries; what a recipe leaves to do is printed on stderr, one `note: <recipe>: …` line each.

#### Recipes
A recipe is a ready-made service with everything it needs. `orobox extend add <recipe>` merges it into the project:

- its services (and volumes) into `.orobox.compose.yaml`, created when missing. A service the file already defines is refused, naming it; `--force` replaces that service and leaves every other one alone;
- its settings into `.orobox.yaml`: list items are appended without duplicates, other keys are only set when the project does not have them. The config must be valid first, since `extend` runs without the usual config check;
- its variables into the `.env` next to `.orobox.yaml` (created when missing), only the keys that file does not define. They reach the stack through the [env merge](configuration.md#overriding-the-generated-env-files);
- its files into `docker/<recipe>/`, never overwriting one. When that directory lies inside the image build context (the directory of `image.dockerfile`), a warning says so: changes there would rebuild the image.

A project value always wins over a recipe value. If any part of a recipe is refused, nothing of that recipe is written. With several recipes (`extend add a b`) they are applied in order, and the ones before a refused recipe stay applied; the ones after it are not attempted. Images are pinned, so a recipe added today runs the same image in a year. After the receipts, each recipe prints what is left to do.

| Recipe | What it adds | Notes printed |
| --- | --- | --- |
| `varnish` | A `varnish` service in front of `web` on http://localhost:6081 (`dev.orobox.url` label), with its VCL in `docker/varnish/default.vcl` | How to point Oro's HTTP cache invalidation (FOSHttpCacheBundle) at Varnish |
| `selenium` | `selenium/standalone-chrome` with noVNC on http://localhost:7900 (`dev.orobox.url` label) | The `BEHAT_PARAMS` / Mink settings that point Behat at it. The image's headless Chromium stays the default; this recipe is for watching a run |
| `sftp` | `atmoz/sftp` on port 2222 with an `orobox` user and a `docker/sftp/upload` bind mount | Host, port and credentials for an integration's SFTP settings |
| `blackfire` | A `blackfire` agent service, `image.php_extensions: [blackfire]`, `php_ini.blackfire.agent_socket`, empty `BLACKFIRE_SERVER_ID` / `BLACKFIRE_SERVER_TOKEN` in `.env` | Where to set the credentials |

Recipes publish fixed host ports (varnish 6081, selenium 7900, sftp 2222), so two projects running the same recipe at the same time collide on them. Change the port in `.orobox.compose.local.yaml` with `ports: !override [...]` for the service; the container side stays as the recipe defines it (varnish `80`, selenium `7900`, sftp `22`), for example `ports: !override ["6082:80"]` for varnish.

Each recipe, what it configures and how to use it is described in [Ready-made services](customization.md#ready-made-services-recipes).

When `php_ini` in `.orobox.yaml` is the path of an ini file of your own, Orobox does not edit that file: the rest of the recipe is applied, and the directives the file does not set yet are printed as lines to add to it by hand. The check runs only when the recipe is added; adding it again, with `--force` because its service now exists, replaces the service with the recipe's and reports nothing left to do once the directives are in the file.

### 17. Test Environment (`test-init`)
Creates, or resets, the test database that `orobox test` runs against: starts `db-test` (plus
Redis, RabbitMQ, Elasticsearch and Mailpit when they are enabled), drops the test database and
installs OroCommerce in the test environment.
```bash
orobox test-init
orobox test-init --tmpfs --tmpfs-size 2g
```
When the test database is already installed, it asks before resetting it. At the end it prints the
connection details of the test database, with the host port configured as `ports.db_test`
(default `5433`).

Options:
- `--tmpfs`: Keep the test database in RAM. This also sets `test.use_tmpfs: true` and
  `test.tmpfs_size` in the config file in use (`.orobox.yaml`, or the one given with `--config`),
  so later runs keep using it. Only those two keys are touched: comments and every other key stay
  as they are (blank lines and indentation are normalized).
- `--tmpfs-size`: Size of the tmpfs mount (default `1g`).

### 18. Database Backup and Restore (`db`)
Dumps the development database to a file, or loads one back.
```bash
orobox db backup var/backup.sql
orobox db restore var/backup.sql
```
- `backup <file>` runs `pg_dump --clean --if-exists` in the `db` container and writes the SQL to
  `<file>` on the host. A failed dump removes the partial file.
- `restore <file>` starts `db` and `application` if needed and stops the services that boot an
  Oro kernel (`web`, `php-fpm-app`, `ws`, `consumer`, `cron`) for the duration, so nothing works
  against a database that is being replaced. It then empties the database, recreates the
  `uuid-ossp` extension, loads the file with `psql`, sets Oro's `application_url`, `url` and
  `secure_url` to the first configured domain (with its port) so a dump taken from another
  environment opens on yours, clears `var/cache/dev`, runs `oro:platform:update --force` (so the
  dump's schema is migrated to the code you have), and starts the stopped services again.

### 19. Update Orobox (`self-update`)
Replaces the running binary with the latest release for your platform. When a newer release was
installed, it then pulls the newer versions of the published Orobox images you have locally.
```bash
orobox self-update
```
A project's [image layer](configuration.md#image-customization-image) is not rebuilt by
`self-update`; the next command that starts containers notices the new base image and rebuilds it.
