# Design: Project-level customization of the image and the stack

**Date:** 2026-10-01
**Status:** all decisions closed — awaiting spec review

## Problem

Orobox runs a published image and a generated Docker Compose stack. Both are the same for every
project, and that is the point: one command gives a working OroCommerce environment. Real projects,
though, always need something more — a system library, a PHP extension, a php.ini value, a MinIO or
Varnish container, a volume with fixtures, an extra environment variable on the consumer.

Today the only lever is the `dockerfile` key (see `internal/docker/customimage.go`). It works well
as an escape hatch, but everything else is closed:

- **Adding one package needs a whole Dockerfile.** Even `apk add imagemagick` means writing a file
  with the mandatory `ARG OROBOX_BASE_IMAGE` / `FROM ${OROBOX_BASE_IMAGE}` header.
- **PHP extensions are hard to add.** `install-php-extensions` exists only in the `builder` stage of
  `templates/docker/Dockerfile`, and only for PHP 8.4/8.5. The final image has neither the
  installer nor `$PHPIZE_DEPS`. The example in `docs/configuration.md` (`RUN install-php-extensions
  redis`) therefore fails on every published image.
- **php.ini changes need an image rebuild**, because the only way in is a `COPY` into `conf.d`.
- **The compose stack cannot be extended.** The files are generated into the internal directory
  (`~/.config/orobox/<project>/`) and rewritten on every command, so hand edits are lost. There is
  no way to add a service, a volume, a mount or an environment variable.
- **Host ports are hardcoded** (5432, 6379, 3000, 8025, 8081, 9200, 5601, 15672). Two Orobox
  projects cannot run at the same time.
- **A local `.env` replaces the whole template** (`writeEnvFile` in `internal/docker/compose.go`):
  changing one variable means copying and maintaining all of them.
- **`orobox logs` knows a fixed list of services** (`cmd/logs.go`), so a service the project adds
  would be invisible to it.

## Goal

A project can add anything it needs to its environment — packages, PHP extensions, PHP settings,
services, volumes, mounts, environment variables — and the common cases need **no Docker
knowledge and no boilerplate**.

Non-goals:

- Re-describing Docker Compose inside `.orobox.yaml`. Compose already is the declarative language
  for services; a second, partial copy of it would be the "macchinoso e ripetitivo" this design
  exists to avoid.
- Letting a project replace the Orobox base image or the core services' wiring. `oro_version`
  stays the only thing that decides which Oro image is underneath.

## Principle: three progressive levels

Each level is the escape hatch of the one before it. A project uses the lowest level that covers
its need, and never has to learn the next one until it does.

| Need | Level | Knowledge needed |
| --- | --- | --- |
| Package, PHP extension, php.ini value | 1 — declarative keys in `.orobox.yaml` | none |
| Multi-stage build, compiled tool, anything exotic | 1b — `image.dockerfile` (today's `dockerfile`) | Dockerfile |
| New service, volume, env var, mount, port | 2 — compose override file | Docker Compose |

## Level 0 — prerequisite: an extension-friendly base image

The published image ships `install-php-extensions` in its **final** stage, at a pinned version, for
every PHP line. The tool installs and removes its own build dependencies, so a project never deals
with `$PHPIZE_DEPS`, `pecl` or `docker-php-ext-configure`.

This is a change to `templates/docker/Dockerfile` (and therefore a rebuild of every published
image through `.github/workflows/docker-build.yml`). It also fixes the broken example in
`docs/configuration.md`.

> **Decision D1 (closed):** pinned version, final stage, all PHP lines. The pin is a build `ARG`
> in the template so a bump is a one-line change.

## Level 1 — declarative image keys

```yaml
image:
  dockerfile: docker/Dockerfile      # optional, see "Combined with a project Dockerfile"
  apk: [imagemagick, poppler-utils, ghostscript]
  php_extensions: [redis, imagick, xsl]
  npm: ["@playwright/test"]          # npm install -g
  run:                               # extra RUN lines, in order
    - curl -sSL https://example.com/tool -o /usr/local/bin/tool && chmod +x /usr/local/bin/tool
```

Orobox renders these keys into a Dockerfile layer on top of the published image:

```dockerfile
ARG OROBOX_BASE_IMAGE
FROM ${OROBOX_BASE_IMAGE}
USER root
RUN apk add --no-cache imagemagick poppler-utils ghostscript
RUN install-php-extensions redis imagick xsl
RUN npm install -g @playwright/test
RUN curl -sSL https://example.com/tool -o /usr/local/bin/tool && chmod +x /usr/local/bin/tool
```

One `RUN` per key, in a fixed order (`apk`, `php_extensions`, `npm`, `run`), so a change to one key
reuses the cached layers before it.

### `image.dockerfile` and the old top-level key

Every image customization lives under one `image:` block, so `dockerfile` moves to
`image.dockerfile`. Its semantics are unchanged: path relative to `.orobox.yaml`, inside the
project, final stage `FROM ${OROBOX_BASE_IMAGE}`.

The top-level `dockerfile` key stays readable as a deprecated alias:

- Only top-level set: used as `image.dockerfile`, with a one-line deprecation warning per command.
- Both set: `Validate` fails, naming both keys, rather than guessing which one wins.
- `SaveConfig` (used by `deploy-init`) always writes `image.dockerfile`, so the first rewrite of the
  file migrates it.

### Combined with a project Dockerfile

When `image.dockerfile` and any other `image.*` key are set, the generated lines are **appended to
the project's Dockerfile** — that is, to its final stage, which `checkExtendsBaseImage` already guarantees is `FROM ${OROBOX_BASE_IMAGE}`.
The result is passed to `docker build -f -` with the project Dockerfile's directory as context.
One build, one layer chain, no second image.

When only the declarative keys are set, the context is an empty directory under the internal
directory.

### Rebuild detection

The existing mechanism stays. `customLayerHash` additionally digests the rendered Dockerfile text,
so a change to any `image.*` key invalidates the layer exactly like a change to the Dockerfile.
`ProjectImageRefs`, `CustomImageRef`, `pull_policy: never` and `orobox up --rebuild` are unchanged:
"has a custom layer" now means "any `image.*` key is non-empty" (the deprecated top-level
`dockerfile` included).

### Validation

`OroConfig.Validate` rejects empty strings in the lists and, for `apk` / `php_extensions` / `npm`,
entries containing whitespace or shell metacharacters (those belong in `run`). It does not check
that a package or extension exists — the build reports that, with its own log, as it does today.

> **Decision D2 (closed):** `image.apk`, `image.php_extensions`, `image.npm`, `image.run`, and
> `image.dockerfile`, with top-level `dockerfile` kept as a deprecated alias.

## Level 1 — PHP settings without a rebuild

```yaml
php_ini:
  memory_limit: 4G
  max_execution_time: 0
  xdebug.log_level: 0
```

Orobox renders the map into `zz-project.ini` in the internal directory and bind-mounts it,
read-only, into `/usr/local/etc/php/conf.d/zz-project.ini` through the shared `volumes-oro` anchor,
so every PHP service (application, php-fpm-app, ws, consumer, cron) gets it. The setup file's
`install` service mounts its own `volumes-setup` anchor, so the mount is added there too: the
Oro install must run with the same settings as the stack. The `zz-` prefix sorts
it after `oro-custom.ini`, so project values win.

A change needs a container recreate, not a build: compose sees the changed mount on the next
`orobox up`, and `EnsureDockerCompose` already reports the change.

`php_ini` also accepts a string: the path, relative to `.orobox.yaml`, of an ini file the project
already has. That file is mounted as-is in place of the generated one, under the same validation as
`image.dockerfile` (relative, inside the project, must exist).

```yaml
php_ini: docker/php.ini
```

Rendering rules for the map form: keys are written verbatim; booleans become `On` / `Off`; numbers
are written as-is; strings are double-quoted when they contain characters php.ini treats specially
(`;`, `=`, `"`, `{`, `}`, `|`, `&`, `~`, `!`, `[`, `(`, `)`, `^`, or surrounding whitespace).
Nested maps are rejected by `Validate`: php.ini has no nesting, and `xdebug.mode` is written as a
dotted key.

php_ini stays a top-level key, not part of `image:`, because it is not baked into the image.

> **Decision D3 (closed):** bind-mounted, map or file path.

## Level 2 — compose override file

A file next to `.orobox.yaml`, found by convention, is appended as the **last** `-f` in
`GetBaseComposeArgs`. Docker Compose's own merge rules apply, so there is no new concept to learn:

```yaml
# .orobox.compose.yaml
services:
  minio:
    image: minio/minio
    command: server /data --console-address :9001
    ports: ["9001:9001"]
    volumes: [minio_data:/data]
    labels:
      dev.orobox.url: http://localhost:9001

  consumer:
    environment:
      ORO_MQ_PREFETCH: "5"

  php-fpm-app:
    volumes:
      - ./docker/fixtures:/var/www/oro/fixtures:ro

volumes:
  minio_data:
```

Two files, both next to `.orobox.yaml`, both optional, both discovered by convention with no
config key:

| File | Committed | Purpose |
| --- | --- | --- |
| `.orobox.compose.yaml` | yes | The team's additions to the stack |
| `.orobox.compose.local.yaml` | no (git-ignored) | One developer's tweaks: ports, personal mounts |

They are appended in that order, after Orobox's own files, so the local file wins over the team
file and both win over the generated stack. `orobox create` adds `.orobox.compose.local.yaml` to the
`.gitignore` it scaffolds; existing projects add it themselves (the docs say so).

### Relative paths

`GetBaseComposeArgs` passes `--project-directory <internal dir>`, and Compose resolves every
relative path in every `-f` file against it. Written as-is, `./docker/fixtures` would resolve inside
`~/.config/orobox/<project>/` and Docker would silently mount an empty directory.

**Decision D5 (closed): Orobox rewrites them.** The user writes paths relative to the file, as in
any compose project, and never sees the internal directory.

`EnsureDockerCompose` reads each override file into a `yaml.v3` node tree, rewrites the relative
paths in place against the directory holding `.orobox.yaml`, and writes the result to the internal
directory (`compose.project.resolved.yaml`, `compose.local.resolved.yaml`) with the same
write-only-on-change rule as `writeComposeFile`. `GetBaseComposeArgs` passes the resolved copies.
Working on the node tree, not on decoded structs, keeps the `!override` / `!reset` tags and every
key Orobox does not know about exactly as written.

Rewritten keys:

| Key | Form |
| --- | --- |
| `services.*.volumes[]` | short syntax `SRC:DST[:MODE]`, when `SRC` starts with `.` or `~` |
| `services.*.volumes[]` | long syntax with `type: bind`, its `source` |
| `services.*.build` | string form, or `build.context` (`build.dockerfile` is relative to the context and is left alone) |
| `services.*.env_file` | string, list of strings, or list of `{path: ...}` |
| `services.*.extends.file` | string |
| `services.*.label_file` | string or list |
| `configs.*.file`, `secrets.*.file` | string |
| `include[]` | string, or `path` / `env_file` / `project_directory` of the long form |

Left alone: absolute paths, values starting with `$` (interpolated, the user's responsibility),
and short-syntax volume sources that are not paths — those are named volumes. `~` expands to the
host user's home directory, as Compose itself does.

### Merge caveats (documented, not worked around)

- Compose **appends** sequences such as `ports`. Replacing one needs `!override` and removing one
  needs `!reset` (Compose ≥ 2.24). The docs show both.
- `orobox up --clean` and `orobox clean` run `down -v`, which deletes the project's own named
  volumes too. Data that must survive belongs in a volume declared `external: true`.
- A `build:` section in the override is built by `compose up` when the image is missing, but not
  rebuilt on change. Orobox passes `--build` to `up` when the override contains any `build:` key.

> **Decision D4 (closed):** `.orobox.compose.yaml` + `.orobox.compose.local.yaml`, by convention.

## Level 2 helpers

### Configurable host ports

**Decision D6 (closed):** a `ports:` map in `.orobox.yaml`, one key per published port.

```yaml
ports:
  http: 8090
  https: 8453
  db: 5434
  redis: 6380
```

| Key | Service | Default (today's value) |
| --- | --- | --- |
| `http`, `https` | web | 8080, 8443 |
| `db` | db | 5432 |
| `db_test` | db-test | 5433 |
| `redis` | redis | 6379 |
| `redisinsight` | redisinsight | 8001 |
| `mail_ui`, `mail_smtp` | mail | 8025, 2025 |
| `rabbitmq`, `rabbitmq_ui` | rabbitmq | 5672, 15672 |
| `elasticsearch` | elasticsearch | 9200 |
| `kibana` | kibana | 5601 |
| `adminer` | adminer | 8081 |
| `gotenberg` | gotenberg | 3000 |

Values are rendered straight into the templates by Go's `text/template`, **not** through Compose
interpolation. The internal `.env` — which Compose also reads for interpolation, because it sits in
the project directory — already defines `ORO_DB_PORT=5432` as the *container* port the application
connects to; a `${ORO_DB_PORT}` host-port variable would collide with it.

`0` means "do not publish this port on the host": the service stays reachable on the compose
network. Unknown keys fail `Validate`, so a typo does not silently keep the default.

The existing `nginx_http_port` / `nginx_https_port` keys and the `ORO_NGINX_HTTP(S)_PORT`
environment variables keep working in `GetNginxPorts`, below `ports.http` / `ports.https` in
precedence.

### `.env` merge

**Decision D7 (closed): merge key by key.** A `.env` (or `.env.test`) next to `.orobox.yaml` holds
only the keys the project changes or adds:

```dotenv
ORO_MAILER_DSN=smtp://mail:1025
MY_API_KEY=xyz
```

`writeEnvFile` renders the template first, then applies the project file over it:

- A key the template already defines is replaced **on its own line**, so the template's order and
  comments survive and the generated file stays readable.
- A key the template does not define is appended, after a `# From <path>` comment.
- Values are copied verbatim, `${...}` references included: Symfony's Dotenv resolves them later,
  and a reference to a template key resolves against the merged file.
- Comments and blank lines of the project file are dropped; only assignments matter.

Compatibility: a project that today keeps a complete copy of `.env` gets the same result, because
every key it defines still overrides the template. What changes is that template keys it does
*not* define — new ones in a later Orobox release — now reach it instead of being missing.

For `project` and `demo` installs nothing else changes: the merged file is what the first
`orobox init` seeds into `.env-app.local` / `.env-app.test`, and later runs still leave those files
alone.

### CLI awareness of project services

- **`orobox logs [service...]`** takes service names as positional arguments, any compose service
  included. The existing flags (`--nginx`, `--php`, `--app`, `--consumer`, `--cron`, `--ws`) keep
  working and add to the list. No argument and no flag still prints the help.
- **`dev.orobox.url` label.** After the built-in URLs, `orobox up` prints one line per override
  service that carries the label, under a "Project services" title:
  `minio: http://localhost:9001`. The labels are read from the resolved override node trees
  (both the map and the list form of `labels`), so no Docker call is needed.
- **Built-in URLs follow `ports:`.** The URLs and the "External Database Connection" port that
  `orobox up` prints today are hardcoded (`cmd/up.go`); they are read from the same port values as
  the templates, so a remapped port is never advertised wrong. A port set to `0` is not printed.
- `commands[].service` and `orobox shell <service>` already accept any service and need no change.

### `orobox extend` — scaffolding

A command group that writes the files this design introduces, so nobody has to remember the
required header or the file names.

| Command | Effect |
| --- | --- |
| `orobox extend image` | Creates `docker/Dockerfile` with the `ARG` / `FROM ${OROBOX_BASE_IMAGE}` header and commented examples, and sets `image.dockerfile` in `.orobox.yaml` |
| `orobox extend compose` | Creates `.orobox.compose.yaml` with commented examples (new service, env on a core service, extra mount, `!override` on ports) |
| `orobox extend compose --local` | Creates `.orobox.compose.local.yaml`, and appends it to `.gitignore` when the file exists and does not list it |
| `orobox extend add` | Lists the available recipes with a one-line description each |
| `orobox extend add <recipe>...` | Adds one or more recipes, see below |

Rules shared by every subcommand:

- **Never overwrite.** An existing target file is left alone and the command says so; a recipe
  whose service name already exists in `.orobox.compose.yaml` is refused, naming it. `--force`
  replaces only that service.
- **Edit `.orobox.yaml` without losing comments.** `SaveConfig` re-marshals the whole struct and
  drops every comment, so `extend` edits the file as a `yaml.v3` node tree instead, touching only
  the keys it sets — the same technique D5 uses for the override.
- Every change is printed as a one-line receipt per file (`created`, `updated`, `skipped`), and
  `--agent` keeps only those lines.

### Recipes

A recipe is an embedded directory under `templates/recipes/<name>/`:

```
templates/recipes/blackfire/
├── recipe.yaml      # description, notes, and what to merge
└── files/           # optional files copied to docker/<name>/ in the project
```

```yaml
# recipe.yaml
description: Blackfire profiler — agent service plus the PHP probe
compose:                 # merged into .orobox.compose.yaml
  services:
    blackfire:
      image: blackfire/blackfire:2.28.31
      environment:
        BLACKFIRE_SERVER_ID: ${BLACKFIRE_SERVER_ID}
        BLACKFIRE_SERVER_TOKEN: ${BLACKFIRE_SERVER_TOKEN}
config:                  # optional, merged into .orobox.yaml (lists appended, deduplicated)
  image:
    php_extensions: [blackfire]
  php_ini:
    blackfire.agent_socket: tcp://blackfire:8307
env:                     # optional, merged into the project .env (D7), never overwriting a key
  BLACKFIRE_SERVER_ID: ""
  BLACKFIRE_SERVER_TOKEN: ""
notes: |                 # printed after the recipe is added
  Set BLACKFIRE_SERVER_ID and BLACKFIRE_SERVER_TOKEN in .env, then run `orobox up`.
```

(Versions in this document are illustrative; the implementation pins the current release.)

- Images are **pinned**, like `gotenberg` in the generated stack, so a recipe added today behaves
  the same in a year.
- `files/` lands in `docker/<recipe>/` and the compose fragment refers to it with a relative path,
  which D5 resolves like any other.
- `config` and `env` are merged, never replaced: a project value always wins over a recipe value.
- A recipe whose `config` sets `php_ini` keys is refused when the project's `php_ini` is a file
  path: the command names the keys to add to that file by hand instead of guessing how to edit it.
- A recipe is plain data. Adding one is a new directory and a test case, with no Go code.

**Decision D10 (closed): v1 ships four recipes.**

| Recipe | What it adds | Notes printed |
| --- | --- | --- |
| `varnish` | `varnish` service in front of `web`, VCL in `docker/varnish/default.vcl`, labelled URL | How to enable Oro's HTTP cache invalidation towards Varnish |
| `selenium` | `selenium/standalone-chrome` with noVNC, labelled URL | The `BEHAT_*` / Mink settings that point Behat at it. The image's headless Chromium stays the default; this recipe is for watching a run |
| `sftp` | `atmoz/sftp` with a user, a `docker/sftp/upload` bind mount, port 2222 | Host, port and credentials for an integration's SFTP settings |
| `blackfire` | `blackfire` agent service, `image.php_extensions: [blackfire]`, the `php_ini` socket setting, empty credentials in `.env` | Where to set the credentials |

`minio` was considered and left out of v1.

### Override validation

As described in [Error handling](#error-handling): invalid YAML fails the command, a missing host
path and an `image` redefined on a core Oro service print a warning.

## Deploy pipeline parity

**Decision D9 (closed): in scope.** The deploy pipeline builds and tests the project in Dagger
containers started `From(r.plan.Image)` (`internal/pipeline/run.go`), and `r.plan.Image` is the
published tag, hardcoded in `internal/pipeline/plan.go`. Without this section a project with
`image.php_extensions: [redis]` passes locally and fails, or behaves differently, in `orobox deploy`
and in the generated GitLab CI.

Changes:

- **One renderer, two consumers.** The function that renders the custom-layer Dockerfile
  (project Dockerfile + `image.*` lines) and the `zz-project.ini` content moves to a place both
  `internal/docker` and `internal/pipeline` import, and takes the Oro version and install type as
  arguments instead of reading viper. The dev path calls it with the configured type; the pipeline
  with `project`.
- **The pipeline builds the layer in Dagger**, not with `docker build`:
  `Directory(<context>).DockerBuild(...)` with the rendered Dockerfile and the
  `OROBOX_BASE_IMAGE` build argument set to the published tag. Dagger's engine caches the layers,
  so an unchanged layer costs nothing on a warm runner; on a cold GitLab runner it is one build per
  pipeline, which is the price of running what was tested locally.
- **`php_ini` is applied with `WithNewFile`** on `/usr/local/etc/php/conf.d/zz-project.ini` in
  `runner.container`, so every step sees the same settings as the dev stack.
- **Build context source.** The context comes from the same place the pipeline reads
  `.orobox.yaml` from — the host working tree — so configuration and image never come from two
  different revisions. With `deploy.source_dir` set, paths stay relative to `.orobox.yaml`, as in
  the dev environment.
- **No layer configured, no change.** `r.plan.Image` stays the published tag and nothing is built.

The compose override and recipes do **not** reach the pipeline: the pipeline has its own service
list (database, search, broker) and is not a compose stack. The docs say so next to the override
section.

## Data flow

```
.orobox.yaml ──► Validate ──► EnsureDockerCompose
   │                              ├─ render compose / env / nginx (unchanged)
   │                              ├─ render zz-project.ini          (php_ini)
   │                              └─ resolve override paths ──► <internal>/compose.{project,local}.resolved.yaml
   │
   └─► EnsureCustomImage (on up/run/exec/start/create)
          ├─ render Dockerfile: project Dockerfile + image.* lines
          ├─ hash: base image ID + context files + rendered text
          └─ docker build -f - (only when the hash label differs)

orobox deploy ──► pipeline plan
                    ├─ same renderer: Dockerfile + zz-project.ini (type = project)
                    └─ Dagger DockerBuild ──► From(<built layer>) + WithNewFile(zz-project.ini)

GetBaseComposeArgs: -f docker-compose.yml -f setup -f test -f compose.project.resolved -f compose.local.resolved
```

## Error handling

- Build failure: unchanged — the build log is printed (stderr in agent mode) and the command fails
  before any container starts.
- Invalid override YAML: the command fails before compose runs, naming the file and the YAML error.
- Override path that does not exist on the host: a warning naming the path, because Docker would
  otherwise create an empty directory and the failure would surface far from its cause.
- An override that redefines `image` on a core Oro service (`application`, `web`, `php-fpm-app`,
  `ws`, `consumer`, `cron`, `volume-init`, `web-init`): a warning, because it detaches that service
  from `oro_version` and from the custom layer.

## Testing

- `internal/config`: validation of `image.*` and `php_ini` (empty entries, metacharacters, nested
  maps, path outside the project, deprecated `dockerfile` alias and the both-set error).
- `internal/docker`: rendered Dockerfile for each key combination, with and without a project
  Dockerfile; hash changes when any `image.*` key changes and only then; `zz-project.ini`
  rendering and mount; `GetBaseComposeArgs` with and without override files; path resolution for
  every path-bearing compose key.
- `writeEnvFile`: replace in place, append of new keys, verbatim `${...}` values, and a complete
  project `.env` producing the same values as today.
- Templates: every published port renders with its default when no value is set, with the
  configured value when set, and is omitted when set to `0`.
- `cmd`: `logs` with positional services plus flags; `up` printing labelled services and
  remapped ports; every `extend` subcommand, including never-overwrite, `--force`, `.gitignore`
  append, and comment preservation in `.orobox.yaml`.
- Recipes: a table test that loads every embedded recipe, validates its compose fragment with
  `docker compose config` against the generated stack, and checks its images are pinned.
- `internal/pipeline`: plan uses the published tag when no layer is configured; with a layer, the
  runner builds it with the rendered Dockerfile and the base-image build argument, and every step
  container carries `zz-project.ini`.
- e2e (`e2e/fixtures`): one project with `image.php_extensions`, `php_ini` and an override service,
  asserting `php -m`, `php -i` and that the extra service is running.

## Decisions

All decisions were taken with the maintainer on 2026-10-01.

- **D1 — Installer in the base image.** ✅ **Closed:** ship `install-php-extensions` at a pinned
  version in the final stage of every published image, for every PHP line.
- **D2 — Key names and the `dockerfile` key.** ✅ **Closed:** everything under `image:` —
  `apk`, `php_extensions`, `npm`, `run`, `dockerfile`. Top-level `dockerfile` is a deprecated alias.
- **D3 — php.ini delivery.** ✅ **Closed:** bind-mounted into every PHP service, no rebuild;
  `php_ini` is either a map or a path to a project ini file.
- **D4 — Override file names.** ✅ **Closed:** `.orobox.compose.yaml` (committed) and
  `.orobox.compose.local.yaml` (git-ignored), discovered by convention, no config key.
- **D5 — Relative paths.** ✅ **Closed:** option A — Orobox rewrites the override's relative paths to
  absolute on a `yaml.v3` node tree and passes compose a resolved copy.
- **D6 — Port values.** ✅ **Closed:** `ports:` map in `.orobox.yaml`, rendered by Go templates (not
  Compose interpolation); `0` disables publishing.
- **D7 — `.env` merge.** ✅ **Closed:** the project `.env` / `.env.test` is merged key by key over the
  rendered template; replaced keys stay on their original line, new keys are appended.
- **D8 — v1 scope.** ✅ **Closed:** everything — levels 0, 1 and 2, ports, `.env` merge,
  `logs <service>`, the URL label, `orobox extend` scaffolding, recipes and override validation.
- **D9 — CI and deploy parity.** ✅ **Closed:** in scope. Verified: `internal/pipeline/plan.go`
  hardcodes the published image. The pipeline builds the same rendered layer in Dagger and applies
  the same `php_ini`; the compose override stays dev-only.
- **D10 — Recipes in v1.** ✅ **Closed:** `varnish`, `selenium`, `sftp`, `blackfire`.
