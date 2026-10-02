[← Back to README](../README.md) · [Documentation index](README.md)

# Customizing the environment

Orobox runs the same published image and the same generated Docker Compose stack for every
project, and that is what makes `orobox init` and `orobox up` work in one command. Real projects
always need something more: a system library, a PHP extension, a different `memory_limit`, an
object store, a fixtures directory mounted into the application, a port that does not clash with
another project.

This page is the task-oriented guide to adding all of that. The exact rules for every key live in
the reference: [Configuration](configuration.md) and [Commands](commands.md#16-extending-the-environment-extend).

## Pick the right tool

There are three levels. Use the lowest one that covers your need: each level is the escape hatch
of the one before it, and you never have to learn the next one until you need it.

| I want to… | Use | Rebuilds the image? |
| --- | --- | --- |
| Install an Alpine package (`imagemagick`, `poppler-utils`, …) | [`image.apk`](#add-system-packages) | yes |
| Add a PHP extension (`redis`, `imagick`, `xsl`, `blackfire`, …) | [`image.php_extensions`](#add-a-php-extension) | yes |
| Install a global npm tool | [`image.npm`](#install-a-global-npm-tool) | yes |
| Run an arbitrary install command | [`image.run`](#run-an-install-command) | yes |
| Compile a tool, use a multi-stage build, `COPY` files into the image | [`image.dockerfile`](#write-your-own-dockerfile) | yes |
| Change a PHP setting (`memory_limit`, `max_execution_time`, Xdebug options) | [`php_ini`](#change-php-settings) | no |
| Move a host port that is already taken | [`ports`](#move-a-host-port) | no |
| Change or add an environment variable | [`.env` next to `.orobox.yaml`](#change-environment-variables) | no |
| Add a service, a volume, a mount, env on one service | [`.orobox.compose.yaml`](#add-a-service) | no |
| Keep personal tweaks out of git | [`.orobox.compose.local.yaml`](#personal-tweaks) | no |
| Add Varnish, Selenium, an SFTP server or Blackfire | [`orobox extend add`](#ready-made-services-recipes) | depends on the recipe |

`orobox extend` writes the files for you with the right header and commented examples:

```bash
orobox extend image             # docker/image/Dockerfile, and image.dockerfile in .orobox.yaml
orobox extend compose           # .orobox.compose.yaml
orobox extend compose --local   # .orobox.compose.local.yaml (+ .gitignore entry if one exists)
orobox extend add               # list the ready-made services
orobox extend add varnish       # add one
```

## Level 1 — the image

The published image is Alpine Linux with PHP-FPM, nginx, Node.js and Composer. Everything you add
under `image:` is built into one local layer on top of it, tagged
`orobox-custom/<project>:<oro_version>-<type>`. It exists only on your machine; no registry is
involved and nothing has to be pushed.

### Add system packages

```yaml
# .orobox.yaml
image:
  apk: [imagemagick, poppler-utils, ghostscript]
```

The names are [Alpine package names](https://pkgs.alpinelinux.org/packages), installed with
`apk add --no-cache`. A version constraint in apk's own syntax (`ghostscript=10.05.1-r0`) is
accepted. Orobox does not check that a package exists: a typo fails the build, and the build log
says why.

### Add a PHP extension

```yaml
image:
  php_extensions: [redis, imagick, xsl]
```

Extensions are installed with [`install-php-extensions`](https://github.com/mlocati/docker-php-extension-installer),
which the published image ships at a pinned release. It fetches and removes the build
dependencies on its own, so there is no `$PHPIZE_DEPS`, `pecl` or `docker-php-ext-configure` to
deal with. Its README lists every supported extension; a specific version is written
`redis-6.1.0`. If your local copy of the base image is older than the tool, the layer downloads the
same pinned release first, so the key works either way.

### Install a global npm tool

```yaml
image:
  npm: ["@playwright/test"]
```

Installed with `npm install -g`. Quote scoped names: YAML reads a leading `@` as reserved.

### Run an install command

Anything that needs shell syntax goes in `run`. Each entry becomes its own `RUN`, executed after
the other keys, in order:

```yaml
image:
  run:
    - curl -sSL https://example.com/tool -o /usr/local/bin/tool && chmod +x /usr/local/bin/tool
    - mkdir -p /opt/reports && chmod 777 /opt/reports
```

An entry is a single line. Join commands with `&&`; a line break inside the entry, a trailing `\`
or a heredoc (`<<EOF`) is refused, because the following text would become Dockerfile
instructions of its own. A YAML folded scalar is fine for a long line:

```yaml
image:
  run:
    - >
      curl -sSL https://example.com/tool -o /usr/local/bin/tool &&
      chmod +x /usr/local/bin/tool
```

A script that does not fit on one line belongs in [your own Dockerfile](#write-your-own-dockerfile).

The `apk`, `php_extensions` and `npm` lists take plain names only: letters, digits and
`@ . _ + : / = ~ -`, starting with a letter, a digit or `@` (so an entry cannot sneak in an option
like `--allow-untrusted`). Anything else is refused with a hint to move it to `run`.

### How the layer is built

The keys render into a Dockerfile, one `RUN` per key in a fixed order (plus one that fetches
`install-php-extensions` when the base image lacks it), so changing one key reuses the cached
layers before it:

```dockerfile
ARG OROBOX_BASE_IMAGE
FROM ${OROBOX_BASE_IMAGE}
USER root
RUN apk add --no-cache imagemagick poppler-utils ghostscript
RUN command -v install-php-extensions >/dev/null 2>&1 || (curl -fsSL https://github.com/mlocati/docker-php-extension-installer/releases/download/2.12.0/install-php-extensions -o /usr/local/bin/install-php-extensions && chmod +x /usr/local/bin/install-php-extensions)
RUN install-php-extensions redis imagick xsl
RUN npm install -g @playwright/test
RUN curl -sSL https://example.com/tool -o /usr/local/bin/tool && chmod +x /usr/local/bin/tool
```

You never run a build yourself. Whenever Orobox creates containers from the image — `orobox up`,
and `init`, `test-init`, `db restore` or `test` when they have to start a service that is not
running — it first compares a hash of the rendered Dockerfile, of the files in the build context
(path, size and modification time) and of the base image with the label on the existing layer,
and rebuilds only when something differs. When nothing changed, the check is a walk of the build
context and one or two `docker image inspect`.

Commands that work inside containers already running — `test` and `qa` on a running stack,
`shell`, `console`, `run`, `xdebug` — neither rebuild nor switch: **run `orobox up` to move the
running containers to a new layer.**

`orobox up --rebuild` pulls the base image, then builds with `--no-cache`. You only need it for what
Docker's cache cannot see, such as an unpinned `apk add` that should pick up a newer package.

Removing every `image.*` key puts the project straight back on the published image.

### Write your own Dockerfile

For a multi-stage build, a compiled tool, or files `COPY`ed into the image, use a Dockerfile:

```bash
orobox extend image
```

This creates `docker/image/Dockerfile` and sets `image.dockerfile: docker/image/Dockerfile` in
`.orobox.yaml`. A file already at that path is adopted only if it is an Orobox layer (final
`FROM ${OROBOX_BASE_IMAGE}`); a different Dockerfile there (a production image, say) makes the
command refuse rather than build the wrong thing.
The final stage must build on the image Orobox passes in the `OROBOX_BASE_IMAGE` build argument;
earlier stages can use anything:

```dockerfile
ARG OROBOX_BASE_IMAGE

FROM golang:1.24-alpine AS tool
RUN go install example.com/some/tool@latest

FROM ${OROBOX_BASE_IMAGE}
COPY --from=tool /go/bin/tool /usr/local/bin/tool
COPY php-fpm-pool.conf /usr/local/etc/php-fpm.d/zz-pool.conf
```

- The **build context is the Dockerfile's directory** (`docker/image/` here), not the repository
  root. Put the files you `COPY` next to the Dockerfile — and nothing else: every file in the
  context is part of the rebuild check, so runtime data there (recipe directories, fixtures,
  uploads) would rebuild the image and recreate the PHP containers whenever it changes. That is
  why the Dockerfile gets a directory of its own.
- `.dockerignore`: with only a Dockerfile (no other `image.*` key) a `Dockerfile.dockerignore`
  works — Docker uses it instead of `<context>/.dockerignore`; with other `image.*` keys the
  rendered Dockerfile goes in on stdin and only `<context>/.dockerignore` applies.
- A final `FROM` naming anything other than `${OROBOX_BASE_IMAGE}` is refused: with a hardcoded tag,
  `oro_version` would no longer decide which Oro image runs. Because the base arrives as a
  build argument, bumping `oro_version` moves your layer with it and the Dockerfile never needs
  editing for an Oro upgrade.
- The declarative keys still work next to it. Their lines are appended to the end of your
  Dockerfile (its final stage), so there is one build and one image.
- Changing a file in the build context triggers a rebuild, just like changing the Dockerfile.

> Build the image, not the code. Your sources are bind-mounted into the containers at run time, so
> copying them into the image gains nothing and makes every edit a rebuild. Use the image for tools
> and libraries.

## Level 1 — PHP settings

### Change PHP settings

```yaml
# .orobox.yaml
php_ini:
  memory_limit: 4G
  max_execution_time: 0
  date.timezone: Europe/Rome
  xdebug.log_level: 0
```

No image build is involved. Orobox renders the map into `zz-project.ini` and mounts it read-only
at `/usr/local/etc/php/conf.d/zz-project.ini` in every PHP container, including the one that runs
the Oro install during `orobox init`. PHP reads it after the image's own settings, so your values
win.

- Write directives as dotted keys, exactly as php.ini names them (`xdebug.mode`, not a nested
  `xdebug:` map). Case is preserved.
- `true` / `false` become `On` / `Off`.
- Constant expressions stay unquoted, so PHP evaluates them:

  ```yaml
  php_ini:
    error_reporting: E_ALL & ~E_DEPRECATED
  ```

- Words php.ini would turn into `1` or nothing (`None`, `yes`, `no`, `on`, `off`, …) are quoted
  when you write them as strings, so `session.cookie_samesite: None` really sends `None`.
- A value with `$` is written literally, in single quotes: php.ini would otherwise replace
  `${VAR}` with an environment variable.
- Multi-word values, and values with characters php.ini treats specially, are quoted for you
  (`sendmail_path: msmtp -a default` keeps working).
- Values are single-line; nested maps and lists are refused.

Already have an ini file in the repository? Point at it instead, and it is mounted as it is:

```yaml
php_ini: docker/php.ini
```

The file must exist; a wrong path is reported as a configuration error, not as a silently empty
mount.

After a change, run `orobox up`: the PHP containers are recreated with the new settings. (Orobox
stamps them with a label holding a hash of the ini, which is what tells Compose they changed.)

## Level 1 — ports and environment

### Move a host port

Two Orobox projects running at the same time both want 8080, 5432, 6379… Move the ones that clash:

```yaml
# .orobox.yaml
ports:
  http: 8090
  https: 8453
  db: 5434
  redis: 6380
  adminer: 0      # do not publish Adminer on the host at all
```

Only the host side moves; containers keep talking to each other on the usual ports, so no DSN
changes. `0` keeps a service off the host entirely, except for `http` and `https`, which the
application URLs and the websocket depend on. `orobox up` prints the URLs with the ports you set.
The full list of keys and defaults is in [Host ports](configuration.md#host-ports-ports).

`ports` is shared with everyone who uses the repository's `.orobox.yaml`. A port that only clashes
on your machine belongs in your [personal compose file](#personal-tweaks), with `ports: !override`
on the service.

### Change environment variables

Put a `.env` (and `.env.test` for the test environment) next to `.orobox.yaml` (or the file given
with `--config`) holding only the variables you change or add:

```dotenv
ORO_MAILER_DSN=smtp://mail:1025
MY_API_KEY=xyz
```

They are layered over the file Orobox generates: your file is appended after it, in its own
order, so your values win and your `${...}` references resolve (`OPTS="${OPTS} --more"` extends
the generated value); the generated values that use a key you changed are then repeated after
it, so they follow your value. Multi-line quoted values (a PEM key) are copied as they are. Run
`orobox up` to apply.

> **`project` and `demo` installs:** the application (`application`, `php-fpm-app`) reads your
> checkout's own `.env-app.local`, which `orobox init` seeds once from the merged file. A later
> change to `.env` reaches only `ws`, `consumer` and `cron`; change `.env-app.local` for the
> application itself. See [Overriding the generated env files](configuration.md#overriding-the-generated-env-files).

To set a variable on **one** service only, use the compose override instead (next section).

## Level 2 — the compose stack

### Add a service

Write a standard Docker Compose file next to `.orobox.yaml`. Orobox appends it after its own files,
with Compose's normal merge rules:

```bash
orobox extend compose
```

```yaml
# .orobox.compose.yaml
services:
  minio:
    image: minio/minio   # pin a RELEASE.* tag in a real project
    command: server /data --console-address :9001
    environment:
      MINIO_ROOT_USER: minio
      MINIO_ROOT_PASSWORD: minio123
    ports: ["9001:9001"]
    volumes: [minio_data:/data]
    labels:
      dev.orobox.url: http://localhost:9001

volumes:
  minio_data:
```

- The service is reachable from the application as `minio` on the compose network.
- The `dev.orobox.url` label makes `orobox up` list it under "Project services".
- `orobox logs minio` and `orobox shell minio` work like for the built-in services, and a custom
  command in `.orobox.yaml` can target it with `service: minio`.
- A service with a `build:` section is rebuilt on every `orobox up`.

### Mount files or set variables on an existing service

The same file tweaks Orobox's own services:

```yaml
# .orobox.compose.yaml
services:
  php-fpm-app:
    volumes:
      - ./docker/fixtures:/var/www/oro/fixtures:ro

  consumer:
    environment:
      ORO_MQ_PREFETCH: "5"
```

Write host paths relative to the file, as in any compose project. Orobox runs compose from its own
internal directory, so it passes compose a copy of your file with every relative host path made
absolute against the project directory. That includes paths reached through YAML anchors and
`<<` merge keys. Your file is never modified.

Compose **appends** to lists. To replace the ports of a core service, tag the list `!override`;
to remove it, `!reset []` (Docker Compose 2.24.4 or later):

```yaml
services:
  web:
    ports: !override
      - "9090:80"
```

### Personal tweaks

`.orobox.compose.local.yaml` works exactly like the team file, is applied after it, and is meant to
stay out of git:

```bash
orobox extend compose --local   # creates it, and adds it to .gitignore if the project has one
```

Use it for what only your machine needs: a port that clashes with another project of yours, a
mount of a local checkout of a dependency, Xdebug environment on a service.

### Things to know about the override

- **Data in named volumes is deleted by `orobox up --clean` and `orobox clear`**, which run
  `docker compose down -v`. For data that must survive, declare the volume `external: true` and
  create it once with `docker volume create`.
- **A broken file stops every command that runs compose**, naming the file. Orobox never runs the
  stack with your override silently dropped. An empty or comments-only file counts as absent; a
  file with several YAML documents (`---`) works as compose reads it.
- **The personal file may tweak what the team file adds** (a recipe's ports, say), and a `build:`
  in a later file that only adds `args` keeps the context an earlier file gave it.
- **Tweaking a service that is not always there is fine.** An entry for `db-test` (which exists
  only for `orobox test` / `test-init`) or for an optional service you disabled is used where that
  service exists and left out elsewhere, instead of breaking every command. A tweak to a service
  the stack never defines gets a warning; a new service needs an `image` or a `build`.
- **Warnings**, printed once per command: a bind-mount source that does not exist on the host
  (in short syntax Docker creates an empty directory there; in long syntax it refuses to start the
  container), and an `image:` set on one of Orobox's own services (`application`, `web`,
  `php-fpm-app`, `ws`, `consumer`, `cron`, `volume-init`, `web-init`), which detaches it from
  `oro_version` and from your image layer.
- **`dev.orobox.url`** is printed as written (no `${VAR}` interpolation), and a service with
  `profiles:` is not listed, since `orobox up` does not start it.
- **The deploy pipeline does not read these files** (see [below](#what-reaches-the-deploy-pipeline)).

## Ready-made services (recipes)

A recipe is a complete service, with everything it needs, that `orobox extend add` merges into the
project:

```bash
orobox extend add            # list them
orobox extend add varnish    # add one
orobox extend add sftp selenium
```

Adding a recipe never overwrites your work: a service that already exists in
`.orobox.compose.yaml` is refused (`--force` replaces only the recipe's services), settings are
only added where `.orobox.yaml` does not have them, variables only where `.env` does not define
them, and files only where none exists. If any part of a recipe is refused, nothing of it is
written. The project needs its `.orobox.yaml` first (`orobox init`). Every recipe pins its image,
and each prints what is left to do once it is added (on stderr with `--agent`).

A recipe's files go to `docker/<recipe>/`. If your `image.dockerfile` sits in `docker/` itself,
that directory is inside the image build context and every change there would rebuild the image;
`extend add` warns about it. Keep the Dockerfile in a directory of its own (`docker/image/`, the
`extend image` default).

### `varnish`

A [Varnish](https://varnish-cache.org/) HTTP cache in front of `web`.

- Adds the `varnish` service (`varnish:9.1.0`), published on http://localhost:6081 (container port
  80), and its VCL in `docker/varnish/default.vcl`, which you are free to edit.
- Browse through http://localhost:6081 to go through the cache; the usual web port bypasses it.
- To let Oro invalidate the cache, configure FOSHttpCacheBundle in `config/config.yml`:

  ```yaml
  fos_http_cache:
    proxy_client:
      varnish:
        http:
          servers: ['varnish:80']
          base_url: 'localhost:6081'
  ```

  The VCL accepts `PURGE` and `BAN` only from private network addresses, which covers the stack.

### `selenium`

[Selenium standalone Chrome](https://github.com/SeleniumHQ/docker-selenium) with noVNC, to watch a
Behat run in a real browser.

- Adds the `selenium` service (`selenium/standalone-chrome:4.49.0-20260909`) with noVNC on
  http://localhost:7900, no VNC password, and a 2 GB `/dev/shm` (Chrome crashes on Docker's 64 MB
  default).
- The image's headless Chromium stays the default for Behat. To run against this browser instead,
  point Mink at the grid and the application, both as seen from inside the stack:

  ```bash
  export BEHAT_PARAMS='{"extensions":{"Behat\\MinkExtension":{"base_url":"http://web/","sessions":{"first_session":{"oroSelenium2":{"wd_host":"http://selenium:4444/wd/hub"}}}}}}'
  ```

  or set the same `base_url` / `wd_host` keys in your `behat.yml`. Then open http://localhost:7900.
- The pinned tag is published for both amd64 and arm64.

### `sftp`

An SFTP server for integrations that exchange files.

- Adds the `sftp` service ([`atmoz/sftp`](https://github.com/atmoz/sftp), pinned by digest because
  the image publishes no versioned tags) on host port 2222, with user `orobox` / password `orobox`
  (uid/gid 1000, so files stay writable on a typical Linux host), and the upload directory
  `docker/sftp/upload` bind-mounted.
- From an integration running in the stack: host `sftp`, port `22`, user `orobox`, password
  `orobox`, remote directory `/upload`.
- From the host: `sftp -P 2222 orobox@localhost`. Uploaded files land in `docker/sftp/upload`.
- The image is built for amd64 only; on arm64 it runs under emulation, if at all.

### `blackfire`

The [Blackfire](https://www.blackfire.io/) profiler: agent service plus PHP probe.

- Adds the `blackfire` agent service (`blackfire/blackfire:2026.9.1`), `blackfire` to
  `image.php_extensions` (so the image is rebuilt with the probe), `blackfire.agent_socket:
  tcp://blackfire:8307` to `php_ini`, and empty `BLACKFIRE_SERVER_ID` / `BLACKFIRE_SERVER_TOKEN`
  to the `.env` next to `.orobox.yaml`.
- Fill in the two variables with the server credentials from
  https://blackfire.io/my/settings/credentials, then run `orobox up`. Keep them out of git if your
  `.env` is committed.
- When `php_ini` is the path of an ini file of your own, Orobox does not edit that file: the rest
  of the recipe is applied and it prints the line to add yourself
  (`blackfire.agent_socket = tcp://blackfire:8307`). Once the line is there, nothing more is
  reported.

### Recipe ports

Recipes publish fixed host ports (6081, 7900, 2222), so two projects running the same recipe at
the same time collide. Move yours in `.orobox.compose.local.yaml`:

```yaml
services:
  varnish:
    ports: !override
      - "6082:80"
```

## When changes take effect

| You change… | What happens | What to run |
| --- | --- | --- |
| an `image.*` key, a file in the Dockerfile's directory, or `oro_version` | the layer is rebuilt the next time Orobox creates containers; running containers stay on the old one until `up` recreates them | `orobox up` |
| `php_ini` (map or the file it points to) | the PHP containers are recreated | `orobox up` |
| `ports` | the affected containers are recreated with the new ports | `orobox up` |
| `.env` / `.env.test` next to `.orobox.yaml` | the generated env files are regenerated (for `project` / `demo`, the application itself reads `.env-app.local`) | `orobox up` |
| `.orobox.compose.yaml` / `.orobox.compose.local.yaml` | Compose recreates the services whose definition changed | `orobox up` |
| an upstream package you install unpinned | nothing (Docker's cache hides it) | `orobox up --rebuild` |

Nothing here needs `orobox init` again.

## What reaches the deploy pipeline

`orobox deploy`, the generated GitLab CI and the Dagger engine of `orobox qa` / `orobox test` run on
the **same image layer and the same PHP settings** as your development stack: Dagger builds the
Dockerfile Orobox renders from `image.*` and writes the same `zz-project.ini` into every step. A
project that needs `redis` locally therefore has it in CI too. The layer is built as a step of its
own, before the dependencies, so a broken entry is reported as such. `--no-cache` rebuilds it. The
deploy summary shows a `Layer:` and a `php.ini:` line when they apply; both come from your working
tree, so with `orobox deploy` building another ref, the summary says the code and the image
settings come from different places.

The compose override, the recipes' services, `ports` and the `.env` merge describe the development
stack only and do not reach the pipeline, which has its own services. See
[The image the pipeline runs on](deployment.md#the-image-the-pipeline-runs-on).

## Troubleshooting

**The build fails.** The build log is printed in full (on stderr with `--agent`). The usual causes
are a package name that does not exist in the Alpine release of the image, or an extension
`install-php-extensions` does not support for that PHP version. Run with `--debug` to stream the
build as it happens.

**My change to the image is not picked up.** The layer is rebuilt when Orobox creates containers,
and running containers keep the old one: `orobox test`, `qa`, `shell`, `console` and `run` work in
the containers that are already up. Run `orobox up`. If the change is an unpinned upstream
package, use `orobox up --rebuild`.

**`install-php-extensions: not found`.** With `image.php_extensions` the layer downloads the tool
when the base image lacks it, so there it means the download failed — check network access from
the build. In a Dockerfile of your own (`RUN install-php-extensions …`) nothing is downloaded for
you: your local copy of the base image predates the tool, and `orobox up --rebuild` refreshes it.

**The image rebuilds and the PHP containers restart every time.** Something in the build context
keeps changing — a recipe directory, fixtures or uploads next to the Dockerfile. Move the
Dockerfile to a directory of its own (`docker/image/`) and update `image.dockerfile`.

**A mounted directory is empty inside the container.** The host path does not exist where Orobox
resolved it — Orobox prints a warning naming the absolute path. Remember that relative paths are
relative to the override file, which sits next to `.orobox.yaml`.

**"port is already allocated".** Another project or program uses that host port. Move it with
[`ports`](#move-a-host-port), or with `!override` in `.orobox.compose.local.yaml` for a service of
your override.

**Every command fails naming `.orobox.compose.yaml`.** The file is not valid YAML, or its top level
is not a mapping. Fix the file; nothing runs with a broken override.

**"the top-level `dockerfile` key is deprecated".** Move it under `image:` as
`image.dockerfile`. Setting both is an error.

**`orobox extend` reformatted my `.orobox.yaml`.** Editing the file keeps its comments, but blank
lines are dropped and indentation is normalized. Review the diff before committing.
