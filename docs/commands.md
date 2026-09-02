[← Back to README](../README.md) · [Documentation index](README.md)

# Command Usage

The main command is `oro` (or `orobox`, depending on how you installed it).

QA (`qa-init`, `qa`) and deployment (`deploy-init`, `ci-init`, `deploy`) commands have their own
pages: [QA tools](qa.md) and [Deployment](deployment.md).

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
The command dynamically generates the `docker-compose.yml` file, starts the services, and proceeds with the environment installation or update.

### 4. Stop Environment (`down`)
Shuts down the Docker services associated with the bundle.
```bash
orobox down
```

### 5. Shell Access (`shell`)
Accesses a container in interactive mode (default: php).
```bash
orobox shell
```

### 6. View Logs (`logs`)
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

### 11. Total Cleanup (`clean`)
Removes all associated containers and volumes to start from scratch.
```bash
orobox clean
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
