[← Back to README](../README.md) · [Documentation index](README.md)

# Internal structure and development

## Internal Structure

### The services Orobox runs

The generated stack typically includes:
- **Nginx** (`web`): web server configured for OroCommerce, HTTPS termination, websocket proxy.
- **PHP-FPM / PHP-CLI** (`php-fpm-app`, `application`, `ws`, `consumer`, `cron`): runtime for the
  application, the websocket server, the message consumer, cron and Symfony commands. They all
  run the same image: the published one, or the project's [image layer](configuration.md#image-customization-image).
- **PostgreSQL** (`db`, and `db-test` for the test environment): main database.
- **Redis**: cache and sessions (optional).
- **RabbitMQ**: message broker (optional).
- **Elasticsearch/OpenSearch** and **Kibana**: search (optional).
- **Mailpit** (`mail`): captures emails sent during development (optional).
- **Adminer**, **RedisInsight**: admin UIs (optional).
- **Gotenberg**: PDF generation.
- Any service the project adds through a [compose override](configuration.md#extending-the-stack-oroboxcomposeyaml)
  or a [recipe](customization.md#ready-made-services-recipes).

The compose files, env files, nginx configuration and `zz-project.ini` are generated into the
internal directory (`~/.config/orobox/<project>/` on Linux, `.orobox/` in CI or with
`OROBOX_LOCAL_CONFIG` set) on every command; editing them by hand has no lasting effect.

### Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra commands, one file per command; thin wrappers over `internal/` |
| `internal/config/` | `.orobox.yaml` schema, validation, version table per Oro release; `phpini.go` (the `php_ini` key), `ports.go` (the `ports` key and defaults) |
| `internal/docker/` | Compose generation and execution. `compose.go` renders the templates and runs compose; `customimage.go` builds the project's image layer and decides when it is stale; `layer.go` renders the layer Dockerfile and `zz-project.ini` (pure functions shared with the pipeline); `override.go` wires the compose override files in; `envmerge.go` merges a project `.env` over the generated one |
| `internal/composeoverride/` | Pure YAML-node processing of a compose override: `Resolve` makes relative host paths absolute (following anchors and merge keys), `Analyze` reports missing bind paths, core-service image redefinitions, `build:` sections and `dev.orobox.url` labels |
| `internal/yamledit/` | Comment-preserving edits of YAML files (set a scalar, append to a list, merge a mapping), used by `orobox extend` on `.orobox.yaml` and `.orobox.compose.yaml` |
| `internal/scaffold/` | File generation: `create project` / `create bundle`, QA and CI files, `extend.go` (`orobox extend image` / `compose`) and `recipes.go` (loading and applying recipes) |
| `internal/pipeline/` | The Dagger pipeline behind `deploy` and the Dagger engine of `qa` / `test`; `layer.go` applies the project's image layer and `php_ini` to the pipeline |
| `internal/qatools/`, `internal/report/` | QA tool configuration and report conversion |
| `templates/docker/` | Dockerfile of the published image, compose templates, entrypoint, nginx, env templates |
| `templates/extend/` | Starting points written by `orobox extend image` / `compose [--local]` |
| `templates/recipes/` | The recipes for `orobox extend add`, one directory each |
| `e2e/` | End-to-end suite against real Docker; see [e2e/README.md](../e2e/README.md) |

Everything under `templates/` is embedded into the binary (`//go:embed all:templates/*` in
`main.go`), so a new template file needs no Go change to be shipped.

### Adding a recipe

A recipe is data, not code. Add a directory under `templates/recipes/<name>/`:

```text
templates/recipes/<name>/
├── recipe.yaml
└── files/            # optional; copied to docker/<name>/ in the project
```

`recipe.yaml` fields (unknown fields are rejected, so a typo cannot silently drop a section):

| Field | Required | Meaning |
| --- | --- | --- |
| `description` | yes | One line, shown by `orobox extend add` without arguments |
| `compose` | yes | A compose fragment (`services`, optionally `volumes`) merged into `.orobox.compose.yaml`. Reference files from `files/` as `./docker/<name>/…` |
| `config` | no | A fragment merged into `.orobox.yaml`: lists are appended without duplicates, scalars set only when absent (e.g. `image.php_extensions`, `php_ini`) |
| `env` | no | Variables appended to the project `.env` when it does not define them |
| `notes` | yes | Printed after the recipe is added: what the user still has to do and how to use the service |

Rules the tests enforce or rely on:

- Every image is pinned to a release tag (never `latest`); an image without versioned tags is
  pinned by digest.
- The compose fragment must be a valid override on top of a stack that has a `web` service; when
  Docker is installed, `TestRecipesComposeValid` checks it with `docker compose config`.
- Give user-facing services a `dev.orobox.url` label so `orobox up` lists them.
- `TestRecipesLoadAndPinned` in `internal/scaffold/recipes_test.go` lists the expected recipe
  names: add yours there, and document the recipe in [docs/customization.md](customization.md#ready-made-services-recipes)
  and the recipe table in [docs/commands.md](commands.md#recipes).

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

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full contributor workflow (lint, unit tests, e2e suite, commit conventions).
