[← Back to README](../README.md) · [Documentation index](README.md)

# Configuration

## Configuration (`.orobox.yaml`)

Orobox uses a configuration file called `.orobox.yaml` in the root of your bundle or project. If the file does not exist, the `init` command will guide you through its interactive creation.

This page is the reference for every key. For a task-oriented walkthrough of adding packages, PHP extensions, PHP settings, ports and services — with worked examples and troubleshooting — see [Customizing the environment](customization.md).

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
image:
  dockerfile: docker/image/Dockerfile
  apk: [imagemagick]
  php_extensions: [redis]
php_ini:
  memory_limit: 4G
  xdebug.log_level: 0
ports:
  http: 8090
  db: 5434
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
- **`project`**: your repository **is** the whole OroCommerce application. Orobox bind-mounts the checkout directly onto `/var/www/oro`, runs `composer install` (resolving dependencies from your repo's own `composer.lock`), then runs the Oro installer. No bundle `namespace`/`class` is needed, and no vendor is synced back to the host. Because the checkout is bound whole, `var/` and its subdirectories (`var/logs`, `var/data`, …) live on the host and are shared with every container — unlike `bundle`, where `var/data` sits in a Docker-managed volume. `var/cache` is a Docker volume in every type, because the Symfony cache is far slower on a bind mount; it is not visible from the host.

  > The project repository must contain a `composer.json` (and ideally a `composer.lock`). If the lock file is absent, Composer resolves from `composer.json`.

  > Private dependencies: declare the repositories in the application's own `composer.json` as usual. Orobox reads that file to decide whether to forward your SSH agent, and `composer.ssh_agent: true` in `.orobox.yaml` forces forwarding when the SSH access is not visible there. Tokens still go in `composer.auth`.

- **`demo`**: identical to `project` in how sources and vendors are wired, but the stack runs production-tuned — `ORO_ENV=prod`, OPcache enabled with `opcache.validate_timestamps=0` (dev types also run OPcache, but keep timestamp checks, every 2 s), `ORO_DEBUG=0`, and no Xdebug compiled into the image. Intended for demo and staging instances, not for development: PHP will not pick up source edits until the container restarts.

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

- **`bundle`**: Orobox bind-mounts its generated `.env` and `.env.test` over the application's `.env-app.local` and `.env-app.test`. Orobox owns those files; edit them through `.orobox.yaml` or by placing a `.env` / `.env.test` next to `.orobox.yaml` (see [Overriding the generated env files](#overriding-the-generated-env-files)).
- **`project` and `demo`**: the checkout owns `.env-app`, `.env-app.local` and `.env-app.test`. On the first `orobox init`, Orobox copies its generated `.env` to `.env-app.local` and `.env.test` to `.env-app.test` **only if those files do not already exist**, then never touches them again — subsequent `orobox up` / `run` / `test` invocations leave your edits alone.

  > **Upgrading an existing `project` setup:** earlier Orobox versions bind-mounted the internal env files over `.env-app.local` and `.env-app.test`, so your checkout may not contain them. Either re-run `orobox init` to have them seeded, or copy them yourself from the Orobox internal directory. If they are missing, the application starts with no database DSN.

  > The `db` and `db-test` containers still read `POSTGRES_*` from Orobox's internal `.env` and `.env.test`. If you change `ORO_DB_*` in your own `.env-app.local`, mirror it there, or the application and the database will disagree on credentials.

#### Overriding the generated env files

A `.env` (or `.env.test`) in the directory that holds the config file (`.orobox.yaml`, or the file given with `--config`) is **merged key by key** over the file Orobox generates; it no longer replaces it. List only the keys you change or add:

```dotenv
ORO_MAILER_DSN=smtp://mail:1025
MY_API_KEY=xyz
```

The merged file is read two ways: Symfony Dotenv reads it in order, resolving `${...}` against what it has read so far, while compose resolves the `env_file` against the project environment, which is this same file read whole. In both, the last assignment of a key wins. The merged file is laid out so that both readings give the values you meant:

- the generated file, unchanged;
- after a `# From .env` comment (`# From .env.test` for the test file), every assignment of your file, in your file's order. It reads as your file would on its own, on top of the generated values: `ORO_APP_DOMAIN=${MY_HOST}` works when `MY_HOST` is a key only your file defines, and `ORO_INSTALL_OPTIONS="${ORO_INSTALL_OPTIONS} --timeout=0"` extends the generated value once. A value that references its own key gets the generated value written in place of the reference, because a reference to itself would mean something else to compose;
- after that, under a `# Values that use the keys above, read again` comment, every generated key that references a key you set and that you do not set yourself, so it follows your value: change `ORO_APP_DOMAIN` and `ORO_APP_URL=http://${ORO_APP_DOMAIN}/` follows. Your own keys that reference one of those are repeated too, and so on until nothing more depends on a key you changed.

In your file:

- Values are copied verbatim, `${...}` references and multi-line quoted values (a PEM key, say) included, except a reference to the key's own name, which is replaced by the generated value as described above.
- Comments and blank lines are dropped; only `KEY=value` assignments matter (an optional leading `export `, leading whitespace and Windows line endings are tolerated, and if a key appears twice the last one wins).

A project that already keeps a complete copy of `.env` keeps working: every key it defines still overrides the generated one. The difference is that keys it does not define, for example ones added by a later Orobox release, now come from the generated file instead of being missing.

For `project` and `demo` installs the merged file is what the first `orobox init` copies to `.env-app.local` / `.env-app.test`, as described above. After that, the `application` and `php-fpm-app` containers read your checkout's own `.env-app.local`: a later change to `.env` reaches only the services that load Orobox's `.env` directly (`ws`, `consumer`, `cron`, and the databases' credentials). Edit `.env-app.local` for the application itself.

### Image customization (`image`)

Orobox runs a published image, so what it ships is the same for everyone. A project that depends on something more — a system library, `pdftotext`, a PECL extension, a CLI tool it shells out to — adds it under `image:`. Most needs fit four declarative keys and no Dockerfile at all:

```yaml
image:
  apk: [imagemagick, poppler-utils, ghostscript]   # apk add --no-cache
  php_extensions: [redis, imagick, xsl]            # install-php-extensions
  npm: ["@playwright/test"]                        # npm install -g
  run:                                             # extra RUN lines, in order
    - curl -sSL https://example.com/tool -o /usr/local/bin/tool && chmod +x /usr/local/bin/tool
```

Orobox renders these keys into a layer on top of the published image, one `RUN` per key in a fixed order (`apk`, `php_extensions`, `npm`, `run`; each `run` entry is its own `RUN`, and `php_extensions` is preceded by a `RUN` that fetches `install-php-extensions` only when the base image lacks it), so a change to one key reuses the cached layers before it. Empty keys are skipped. Orobox does not check that a package exists — the build reports that with its own log.

Validation:

- `apk`, `php_extensions` and `npm` take plain names: letters, digits and `@ . _ + : / = ~ -`, starting with a letter, a digit or `@` (so an entry cannot be an option such as `--allow-untrusted`). Anything else is refused with a hint to move it to `run`.
- A `run` entry is one line. A YAML folded scalar (`- >`) is fine — its trailing newline is dropped — but a line break inside the entry, a trailing `\` or a heredoc (`<<EOF`) is refused, because the following text would become Dockerfile instructions of its own. Join commands with `&&`, or move a longer script into `image.dockerfile`.
- `install-php-extensions` comes with the published image. If your local copy of the base image predates it, the layer downloads the same pinned release before installing the extensions, so `php_extensions` works either way; `orobox up --rebuild` (or `docker pull` of the base) refreshes the base itself.

The layer is built locally, tagged `orobox-custom/<project>:<oro_version>-<type>`, and exists only on your machine. See [Custom Dockerfile](#custom-dockerfile-imagedockerfile) below for what it costs and when it rebuilds; the same rules apply whichever keys you use.

### Custom Dockerfile (`image.dockerfile`)

For anything the declarative keys cannot express — multi-stage builds, `COPY`ing files in — point `image.dockerfile` at a Dockerfile of your own (`orobox extend image` creates one with the required header and sets the key for you, see [`extend`](commands.md#16-extending-the-environment-extend)):

```yaml
image:
  dockerfile: docker/image/Dockerfile
```

> **Deprecated:** the top-level `dockerfile` key is the old spelling of `image.dockerfile`. It still works and is read as `image.dockerfile`, but every command prints a warning asking you to move it. Setting both is an error. `orobox deploy-init` rewrites the file with the new key.

The path is relative to the directory holding `.orobox.yaml`. The file belongs to your repository: commit it, review it, change it like any other source file.

Its **final stage must build on the image Orobox publishes**, which arrives in the `OROBOX_BASE_IMAGE` build argument:

```dockerfile
ARG OROBOX_BASE_IMAGE
FROM ${OROBOX_BASE_IMAGE}

RUN apk add --no-cache imagemagick poppler-utils
RUN install-php-extensions redis
```

The published image already ships [`install-php-extensions`](https://github.com/mlocati/docker-php-extension-installer) (at a pinned release), so adding a PHP extension is a single `RUN` line as above — no need to download the tool yourself.

`ARG` before `FROM` is required by Docker itself — that is the only way a `FROM` can read a build argument. Orobox refuses a Dockerfile whose last `FROM` names anything else, because a hardcoded tag would make `oro_version` decide nothing and break the stack somewhere far from this config key. Bumping `oro_version` in `.orobox.yaml` therefore moves your layer with it, and the Dockerfile never needs editing for an Oro upgrade.

Earlier stages are unconstrained, so compiling something and copying the result over works as usual:

```dockerfile
ARG OROBOX_BASE_IMAGE

FROM golang:1.24-alpine AS tool
RUN go install example.com/some/tool@latest

FROM ${OROBOX_BASE_IMAGE}
COPY --from=tool /go/bin/tool /usr/local/bin/tool
```

How it works, and what it costs:

- **Combined with the declarative keys:** when `image.dockerfile` and any other `image.*` key are both set, the generated lines are appended to the project's Dockerfile, i.e. to its final stage — one build, one layer chain, no second image. The generated lines start with `USER root`, because your Dockerfile may end on a non-root `USER` and installs need root. The layer therefore ends as root; where compose sets `user:` for a service, that is the user it runs as.
- `.dockerignore`: a project Dockerfile built as it is (no other `image.*` key) is passed to `docker build -f <path>`, so a Dockerfile-specific `<name>.dockerignore` works: Docker uses it instead of `<context>/.dockerignore` when it exists. With other `image.*` keys the rendered Dockerfile is passed on stdin, and only `<context>/.dockerignore` applies.
- The **build context is the directory containing the Dockerfile**, not the repository root (`orobox extend image` puts it in `docker/image/`, a directory of its own). With only the declarative keys there is nothing to `COPY`, so the context is an empty directory under Orobox's internal directory. Put the files you `COPY` next to it (`docker/php.ini`, `docker/entrypoint-extra.sh`) — a context scoped this way keeps builds fast and rebuild detection exact.
- Rebuilds are automatic. There is **no separate command and no need to re-run `init`**: whenever Orobox creates containers from the image — `orobox up`, and `init`, `test-init`, `db restore` or `test` when they have to start a service that is not running — it notices that the rendered Dockerfile (so any change to an `image.*` key), a file in its build context, or the base image changed, and rebuilds first. Commands that work inside the containers already running (`test` and `qa` on a running stack, `shell`, `console`, `run`, `xdebug`) neither rebuild nor switch to a new layer: **only `orobox up` moves running containers to it**. `orobox self-update` therefore only pulls, and the layer follows on the next `orobox up`.
- When nothing changed, the check walks the build context (the path, size and modification time of every file in it) and runs one or two `docker image inspect` — no build runs. Because every file there counts, **keep runtime data out of the build context**: recipe directories (`docker/varnish/`, `docker/sftp/upload/`), fixtures and uploads change while the stack runs, and each change would rebuild the image and recreate the PHP containers. `orobox extend add` warns when a recipe's files would land inside it.
- `orobox up --rebuild` pulls the base image first, then builds with `--no-cache`, for what Docker's cache cannot see: an unpinned `RUN apk add` that should pick up a newer package.
- The image is tagged `orobox-custom/<project>:<oro_version>-<type>` and exists only on your machine. It is per checkout, so two projects on the same host never share one.
- Removing every `image.*` key puts the project straight back on the published image, with no local build at all.

> Build the image, not the code. Your sources are bind-mounted into the container at run time, so `COPY`ing them into the image gains nothing and makes every edit a rebuild. Use this for tools and libraries.

### PHP settings (`php_ini`)

To change a PHP setting — a memory limit, a timezone, Xdebug options — for every PHP service, set `php_ini` to a map of directives. No image build is involved:

```yaml
php_ini:
  memory_limit: 4G
  max_execution_time: 0
  xdebug.log_level: 0
```

Orobox renders the map into `zz-project.ini` in its internal directory and bind-mounts it, read-only, at `/usr/local/etc/php/conf.d/zz-project.ini` in every PHP service (`application`, `php-fpm-app`, `ws`, `consumer`, `cron`) and in the `install` service of the setup run, so `orobox init` installs Oro with the same settings the stack runs with. The `zz-` prefix makes PHP read it after the image's own `oro-custom.ini`, so your values win.

A change needs the containers recreated, not rebuilt. The bind-mount path never changes, which Docker Compose does not treat as a reason to recreate anything, so Orobox also stamps every PHP service with a `dev.orobox.php-ini-hash` label holding a short hash of the mounted ini. When the ini changes, the label changes, and the next `orobox up` recreates the containers. In the file form the hash covers the content of your own file, so editing it is picked up the same way.

Rendering rules:

- Directive names are written verbatim, dots and case included (`xdebug.log_level`, not a nested `xdebug:` map). They may contain only letters, digits, `.`, `_` and `-`.
- Booleans become `On` / `Off`; numbers are written as they are.
- **Constant expressions are written unquoted**, so PHP evaluates them: `error_reporting: "E_ALL & ~E_DEPRECATED"` becomes `error_reporting = E_ALL & ~E_DEPRECATED`. A value counts as an expression when it parses as php.ini's expression syntax, uses an operator (`|`, `&`, `^`, `~`, `!`), and its operands are numbers or upper-case constants (`E_ALL`, `E_NOTICE`). A lower-case word joined by an operator (`dev&test`) is a literal and is quoted. PHP evaluates an upper-case name it does not know as `0`, so a misspelled constant (`E_AL & ~E_DEPRECATED`) silently gives the wrong value rather than an error.
- Strings that are php.ini keywords (`none`, `null`, `yes`, `no`, `on`, `off`, `true`, `false`, in any case) are double-quoted, so they reach PHP as the word: `session.cookie_samesite: None` stays `None` instead of becoming empty. Write a real YAML boolean (`true`) for an On/Off setting.
- Strings containing `$` are single-quoted and taken literally; php.ini would otherwise expand `${VAR}` from the environment. A value containing both `$` and `'` cannot be written and is refused.
- Other strings are double-quoted (with `\` and `"` escaped) when they contain whitespace (a multi-word value), `;`, `=`, `#`, `"`, `'`, `{`, `}`, `[`, `]`, operators or parentheses that do not form a valid expression, or when they are empty; anything else is written as it is. A value cannot span several lines.
- php.ini has no nesting, so a nested map or a list value is rejected: write `xdebug.mode` as a dotted key.

If you already keep an ini file in the repository, give its path instead of a map. It is mounted as-is in place of the generated one:

```yaml
php_ini: docker/php.ini
```

The path follows the same rules as [`image.dockerfile`](#custom-dockerfile-imagedockerfile): relative to the directory holding `.orobox.yaml`, inside the project, and free of `:`, `"`, `\`, `$` and control characters (they would break the volume definition). It must also exist and be a file: a missing file is a configuration error reported when the config loads (and by `orobox init`), rather than a mount Docker would fill with an empty directory. `orobox down` and `orobox clear` are the exception — they only stop the environment, so they print a warning and go on.

`php_ini` stays outside `image:` because it is not part of the image.

### Host ports (`ports`)

Every port the stack publishes on your machine can be moved with a `ports` map, for the day `8080` or `5432` is already taken by something else:

```yaml
ports:
  http: 8090
  https: 8453
  db: 5434
  redis: 6380
```

Keys you leave out keep their default. A value of `0` means "do not publish this port on the host" (except for `http` and `https`, see below): the service stays reachable by the other containers on the compose network, it just has no host port. A key that is not in the table, or a number outside 0-65535, is a configuration error, so a typo does not silently keep the default.

| Key | Service | Default | Container port |
| --- | --- | --- | --- |
| `http` | web | 8080 | 80 |
| `https` | web (only with an SSL domain) | 8443 | 443 |
| `db` | db | 5432 | 5432 |
| `db_test` | db-test (`orobox test-init`) | 5433 | 5432 |
| `redis` | redis | 6379 | 6379 |
| `redisinsight` | redisinsight | 8001 | 5540 |
| `mail_ui` | mail (Mailpit web UI) | 8025 | 8025 |
| `mail_smtp` | mail (Mailpit SMTP) | 2025 | 1025 |
| `rabbitmq` | rabbitmq (AMQP) | 5672 | 5672 |
| `rabbitmq_ui` | rabbitmq (management UI) | 15672 | 15672 |
| `elasticsearch` | elasticsearch | 9200 | 9200 |
| `kibana` | kibana | 5601 | 5601 |
| `adminer` | adminer | 8081 | 8080 |
| `gotenberg` | gotenberg | 3000 | 3000 |

Only the host side moves. Containers keep talking to each other on the container ports, so `ORO_DB_PORT`, the Redis and RabbitMQ DSNs and the like need no change. `orobox up` prints the URLs and the database port of the services that are enabled using these values, and leaves out a service whose port is `0`.

`http` and `https` take precedence over the deprecated `nginx_http_port` / `nginx_https_port` keys of `.orobox.yaml` and the `ORO_NGINX_HTTP_PORT` / `ORO_NGINX_HTTPS_PORT` environment variables, which keep working when `ports` does not set them (the keys must be between 1 and 65535). Between those two, the environment variable wins when both are set, as an `ORO_*` variable does for any key. A port left empty (`db: ~`) is a configuration error: write a number, or `0` to not publish it. `http` and `https` are the exception to the `0` rule: they must be between 1 and 65535, because the web port is the application's entry point (the application URLs and the websocket frontend are built from it) and cannot be left unpublished.

Changing a port needs the containers recreated: run `orobox up` again.

### Extending the stack (`.orobox.compose.yaml`)

To add a service of your own (an object store, a mock API), or to tweak one of Orobox's, write a standard Docker Compose file next to `.orobox.yaml`. There is no configuration key: the files are found by name.

| File | Committed | Purpose |
| --- | --- | --- |
| `.orobox.compose.yaml` | yes | The team's additions to the stack |
| `.orobox.compose.local.yaml` | no (git-ignored) | One developer's tweaks: ports, personal mounts |

Both are optional and are appended in that order after Orobox's own compose files, so the local file wins over the team file, and both win over the generated stack. Docker Compose's usual merge rules apply, so there is nothing new to learn:

```yaml
# .orobox.compose.yaml
services:
  minio:
    image: minio/minio
    command: server /data --console-address :9001
    ports: ["9001:9001"]
    volumes: [minio_data:/data]

  consumer:
    environment:
      ORO_MQ_PREFETCH: "5"

  php-fpm-app:
    volumes:
      - ./docker/fixtures:/var/www/oro/fixtures:ro

volumes:
  minio_data:
```

`orobox extend compose` writes the team file with commented examples, and `orobox extend compose --local` writes the personal one and adds it to your `.gitignore` when the project has one (see [`extend`](commands.md#16-extending-the-environment-extend)). `orobox create` also adds `.orobox.compose.local.yaml` to the `.gitignore` of a standalone bundle it generates.

**Relative paths.** Write paths relative to the file, as in any compose project. Orobox runs compose from its own internal directory (`~/.config/orobox/<project>/` on Linux, the platform's user config directory elsewhere, `.orobox/` in CI or with `OROBOX_LOCAL_CONFIG` set), so it rewrites the relative host paths of a copy of your file (`./docker/fixtures` becomes an absolute path under the directory that holds `.orobox.yaml`) and passes that copy to compose as `compose.project.resolved.yaml` / `compose.local.resolved.yaml`. Your file is never modified. The rewritten keys are:

| Key | Forms |
| --- | --- |
| `services.*.volumes` | short syntax `SRC:DST[:MODE]` when `SRC` starts with `.` or `~`; long syntax `type: bind` with its `source` |
| `services.*.build` | the string form, or `build.context` (`build.dockerfile` is relative to the context and left alone; remote contexts such as `https://…`, `git@…` or `github.com/…` are left alone). A `build:` mapping without `context` gets the project directory: compose's default `.` would be Orobox's internal directory |
| `services.*.build.additional_contexts` | the mapping form and the `name=path` list form; `docker-image://`, `service:`, `oci-layout://`, `target:` and URL values are left alone |
| `services.*.build.ssh` | the path of `id=path` entries (a bare `default` names the agent and has no path) |
| `services.*.develop.watch` | each entry's `path` |
| `services.*.env_file` | a string, a list of strings, or a list of `{path: …}` |
| `services.*.extends.file` | string |
| `services.*.label_file` | string or list |
| `configs.*.file`, `secrets.*.file` | string |
| `include` | a string entry, or the `path` / `env_file` / `project_directory` of the long form |

Values reached through YAML anchors and `<<` merge keys are rewritten too, so a `x-mounts: &mounts [...]` fragment shared by several services works as written. A leading `~` expands to your home directory, as compose does (`~bob/x` included). Absolute paths, values starting with `$` (interpolated by compose itself) and short-syntax sources without a path — named volumes — are left as they are. A `$` in the project directory's own path is escaped (`$$`) so compose does not interpolate it, and when that path contains a `:` short-syntax volumes are written in long syntax. A file may hold several YAML documents (separated by `---`); a file that is empty or holds only comments counts as absent.

**Services the stack does not define.** Compose rejects the whole project over a service that has neither an image nor a build context. An entry that only tweaks a service the generated stack does not define for the command at hand — `db-test`, which exists only for `orobox test` and `test-init`, or an optional service the project disabled — is therefore left out of the copy compose reads for that command, and kept where the stack defines it. A service that the stack never defines (a disabled service, a typo) produces a warning; to add a new service, give it an `image` or a `build`. The personal file counts the team file's services as part of the stack, so it can tweak a service the team file (or a recipe) adds. An entry that reaches `services:` only through a YAML merge key (`<<: *extra`) cannot be left out of the copy, so if it would have to be, the command stops and names it.

**Build contexts across files.** A `build:` mapping without `context` gets the project directory (compose's default `.` would be Orobox's internal directory) — unless an earlier document of the same file, or the team file for the personal one, already gives that service a build: compose merges build mappings key by key, so a later `build: {args: …}` keeps the earlier context.

**Merge caveats.**

- Compose **appends** to sequences such as `ports`. To replace the list a core service already has, tag it `!override`; to drop it, tag it `!reset` (both need Docker Compose 2.24.4 or later):

  ```yaml
  services:
    web:
      ports: !override
        - "9090:80"
  ```

- `orobox up --clean` and `orobox clear` run `down -v`, which deletes the named volumes your override declares as well. Data that must survive belongs in a volume declared `external: true` (create it once with `docker volume create`).
- A service with a `build:` section is built by `orobox up` on every run (Orobox passes `--build`), so an edited Dockerfile takes effect.
- Redefining `image` on one of Orobox's own services (`application`, `web`, `php-fpm-app`, `ws`, `consumer`, `cron`, `volume-init`, `web-init`) detaches it from `oro_version` and from the [custom image layer](#image-customization-image); Orobox prints a warning.

**Project service URLs.** Give a service the label `dev.orobox.url` and `orobox up` lists it under "Project services" after the built-in URLs. The value is printed as written (compose interpolation is not applied to it), and a service with `profiles:` is not listed, since `orobox up` does not start it:

```yaml
services:
  minio:
    labels:
      dev.orobox.url: http://localhost:9001
```

Every service of the override can also be named in [`orobox logs`](commands.md#6-view-logs-logs), [`orobox shell`](commands.md#5-shell-access-shell) and in the `service` of a [custom command](commands.md#12-run-custom-commands-run).

**Errors and warnings.** A file that is not valid YAML, or whose top level is not a mapping, stops every command that runs compose, naming the file; Orobox never runs the stack with the override silently dropped. A bind-mount source that does not exist on the host produces a warning naming the path and what Docker will do — in short syntax it creates an empty directory there, in long syntax it refuses to start the container — because either way the problem would show up far from its cause. Sources compose still has to interpolate (`${VAR}`) and long-syntax binds with `bind.create_host_path: true` are not checked. Each warning is printed once per command.

**The deploy pipeline does not use these files.** `orobox deploy` and the generated CI build and test the application image, not your development stack; services added here exist only in local `orobox` commands.

### Configuration Fields

Every command except `init`, `create`, `extend`, `self-update`, `help`, `completion` and `version` checks the configuration first and stops on an error, naming the file and the key: an unknown key, a value of the wrong type, a YAML syntax error, a path outside the project.

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
- `image`: (map, optional) Customizes the image the stack runs. See [Image customization](#image-customization-image).
    - `dockerfile`: (string) Path, relative to `.orobox.yaml`, of a project-owned Dockerfile that extends the published image. See [Custom Dockerfile](#custom-dockerfile-imagedockerfile).
    - `apk`: (list of strings) Alpine packages, installed with `apk add --no-cache`.
    - `php_extensions`: (list of strings) PHP extensions, installed with `install-php-extensions`.
    - `npm`: (list of strings) Global npm packages, installed with `npm install -g`.
    - `run`: (list of strings) Extra shell commands, each its own `RUN`, executed last and in order.
- `dockerfile`: (string, **deprecated**) Old spelling of `image.dockerfile`. Every command prints a warning on stderr while it is set (not for `completion`, `help` and `version`, whose output may be piped into a file).
- `nginx_http_port`, `nginx_https_port`: (int, **deprecated**) Old spelling of `ports.http` / `ports.https`, which win when both are set.
- `php_ini`: (map or string, optional) PHP settings applied to every PHP service without rebuilding the image. A map of php.ini directives (`memory_limit: 4G`, `xdebug.log_level: 0`) rendered into a mounted `zz-project.ini`, or the path of an ini file of your own. See [PHP settings](#php-settings-php_ini).
- `ports`: (map of ints, optional) Host port published for each service, `0` to not publish it. See [Host ports](#host-ports-ports).
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
- `--debug` / `-d`: Shows all Docker output.
- `--agent`: Minimal output for automated callers.

#### Agent mode

`--agent` reduces a command's output to what an automated caller — an LLM agent, a script, a CI
job that parses results — actually needs. Everything Orobox says about itself is dropped:
banners, spinners, progress lines, per-tool headers and success confirmations.

What is left:

- **stdout** carries the payload only: QA findings, test failures, and the verbatim output of the
  commands whose child process output *is* the result (`logs`, `shell`, `console`, `run`, `db`).
- **stderr** carries errors, one line each, prefixed `error: `, with no colour. A multi-line
  message gets the prefix on every line, so a caller splitting on newlines cannot mistake a
  continuation for payload.
- The **exit code** is unchanged from a normal run. It is what an agent reads.

A command that succeeds with nothing to report prints nothing at all:

```bash
orobox qa --agent
```

On a clean tree that writes zero bytes and exits 0.

Details worth knowing:

- **`--debug` wins.** Passing both prints everything, as `--debug` alone would. `--debug` exists to
  show the full picture, and silently discarding it would be the worse surprise.
- **Flag only.** There is no environment variable and no auto-detection from a non-terminal
  stdout. Agent mode is never entered because of an inherited shell or a pipe — existing CI
  pipelines and scripts keep the output they have today.
- **Prompts are never shown.** Interactive questions take their default instead of reading stdin,
  so a command cannot hang waiting for an answer nobody is there to give. When a required value
  has no default, the command writes one `error:` line naming what to set and exits non-zero.
- **`--report` still works.** `orobox qa --report=gitlab --agent` writes the report file as usual;
  only the summary on the terminal is suppressed.
