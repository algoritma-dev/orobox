# Stack Customization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a project add packages, PHP extensions, php.ini values, services, volumes, mounts, ports and env vars to its Orobox environment with no boilerplate, and run the same image layer in the deploy pipeline.

**Architecture:** Declarative `image.*` keys render into one Dockerfile layer built by the existing `EnsureCustomImage` machinery; `php_ini` is a generated file bind-mounted into every PHP service; a convention-discovered compose override is path-resolved on a `yaml.v3` node tree and appended as the last `-f`; `orobox extend` scaffolds files and merges embedded recipes with comment-preserving YAML edits; the Dagger pipeline builds the same rendered layer.

**Tech Stack:** Go, cobra, viper, `gopkg.in/yaml.v3`, `text/template`, Docker Compose v2 (≥ 2.24 for `!override`/`!reset`), Dagger Go SDK `dagger.io/dagger v0.21.9`.

**Spec:** `docs/superpowers/specs/2026-10-01-stack-customization-design.md`

## Global Constraints

- **No git commands.** Never run `git add/commit/branch/push`. Each task ends with a checkpoint: list the changed files and stop; the maintainer commits.
- Tests: `go test ./...` must pass after every task; lint with `golangci-lint run ./...`.
- Prose in code comments, docs and error messages: English, matching the surrounding comment density and voice (comments explain *why*).
- Base image argument name stays `OROBOX_BASE_IMAGE` (`config.DockerfileBaseImageArg`).
- Custom layer tag stays `orobox-custom/<project>:<oro_version>-<type>`; label stays `dev.orobox.custom-layer`.
- Layer `RUN` order is fixed: `apk`, `php_extensions`, `npm`, `run`; one `RUN` per non-empty key.
- php.ini file name: `zz-project.ini`, mounted read-only at `/usr/local/etc/php/conf.d/zz-project.ini`.
- Override files: `.orobox.compose.yaml` then `.orobox.compose.local.yaml`, next to `.orobox.yaml`; resolved copies `compose.project.resolved.yaml` / `compose.local.resolved.yaml` in the internal dir.
- URL label key: `dev.orobox.url`.
- Core Oro services (image-redefinition warning): `application`, `web`, `php-fpm-app`, `ws`, `consumer`, `cron`, `volume-init`, `web-init`.
- Port keys and defaults: `http` 8080, `https` 8443, `db` 5432, `db_test` 5433, `redis` 6379, `redisinsight` 8001, `mail_ui` 8025, `mail_smtp` 2025, `rabbitmq` 5672, `rabbitmq_ui` 15672, `elasticsearch` 9200, `kibana` 5601, `adminer` 8081, `gotenberg` 3000. `0` = not published.
- Recipes in v1: `varnish`, `selenium`, `sftp`, `blackfire`; every recipe image pinned (no `latest`, no untagged).
- Allowed characters in `image.apk` / `image.php_extensions` / `image.npm` entries: `^[A-Za-z0-9@._+:/=~-]+$`.

## Review Focus

1. **Dotted php.ini keys through viper.** Viper splits keys on `.` and lowercases them, so `xdebug.log_level: 0` read via `viper.Get` becomes a nested map. Expected: `zz-project.ini` contains `xdebug.log_level = 0`. Pinned by `TestGetPhpIniKeepsDottedKeys` (Task 5).
2. **Upgrade with only the old top-level `dockerfile`.** Expected: same image ref, a deprecation warning, one rebuild (the hash now includes the rendered text), no error. Pinned by `TestEnsureCustomImageDeprecatedAliasSameRef` (Task 4).
3. **Override file empty or comments-only.** Expected: treated as absent — no resolved copy, no `-f`, no error. Pinned by `TestResolveEmptyDocument` (Task 8) and `TestWriteComposeOverridesSkipsEmpty` (Task 9).
4. **Override deleted after a previous run.** Expected: the stale resolved copy is removed and no longer passed to compose. Pinned by `TestWriteComposeOverridesRemovesStale` (Task 9).
5. **Project `.env` with `export KEY=`, quoted values, empty values, CRLF line endings.** Expected: each parsed as an assignment and merged; CRLF does not leak `\r` into values. Pinned by `TestMergeEnvTolerantParsing` (Task 7).

---

## Phase 1 — Image layer

### Task 1: Ship `install-php-extensions` in the published image

**Files:**
- Modify: `templates/docker/Dockerfile` (builder stage `ADD …install-php-extensions`, final stage after `# Copy PHP extensions from builder`)
- Modify: `docs/configuration.md` (Custom Dockerfile section — example now valid; mention the tool is preinstalled)
- Test: `internal/docker/dockerfile_test.go`

**Interfaces:**
- Produces: final image contains `/usr/local/bin/install-php-extensions` for every PHP line; template `ARG INSTALL_PHP_EXTENSIONS_VERSION` (global, before first `FROM`).

- [ ] **Step 1: Write failing test `TestDockerfileShipsExtensionInstaller`**

For each PHP version in `{"8.2","8.3","8.4","8.5"}` (set `PHPVersion` in `dockerfileData`), render the real template and assert:
```go
mustContain(t, out, "ARG INSTALL_PHP_EXTENSIONS_VERSION=")
mustContain(t, out, "releases/download/${INSTALL_PHP_EXTENSIONS_VERSION}/install-php-extensions")
mustNotContain(t, out, "releases/latest/download/install-php-extensions")
// present in the final stage, i.e. after the last FROM
final := out[strings.LastIndex(out, "\nFROM "):]
mustContain(t, final, "/usr/local/bin/install-php-extensions")
```

- [ ] **Step 2: Run** `go test ./internal/docker/ -run TestDockerfileShipsExtensionInstaller -v` — expect FAIL.

- [ ] **Step 3: Implement.** Declare `ARG INSTALL_PHP_EXTENSIONS_VERSION=<current release tag of mlocati/docker-php-extension-installer>` with the other global ARGs, re-declare it in the stages that use it. In the final stage: `ADD https://github.com/mlocati/docker-php-extension-installer/releases/download/${INSTALL_PHP_EXTENSIONS_VERSION}/install-php-extensions /usr/local/bin/` + `chmod +x`. Switch the builder's existing `latest` URL to the same pinned form. Do not change which extensions are compiled.

- [ ] **Step 4: Run** `go test ./internal/docker/ -v` — expect PASS (including the existing Dockerfile tests).

- [ ] **Step 5: Docs.** In `docs/configuration.md` state that `install-php-extensions` is preinstalled; keep the `RUN install-php-extensions redis` example.

- [ ] **Step 6: Checkpoint** — list changed files, stop for maintainer commit. Note for the maintainer: merging triggers `.github/workflows/docker-build.yml` (full matrix rebuild).

### Task 2: `image:` config block, validation and the deprecated `dockerfile` alias

**Files:**
- Modify: `internal/config/config.go` (`OroConfig`, `Validate`, `GetDockerfile`, `GetDockerfilePath`, `SaveConfig`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
```go
type ImageConfig struct {
    Dockerfile    string   `yaml:"dockerfile,omitempty" mapstructure:"dockerfile"`
    Apk           []string `yaml:"apk,omitempty" mapstructure:"apk"`
    PhpExtensions []string `yaml:"php_extensions,omitempty" mapstructure:"php_extensions"`
    Npm           []string `yaml:"npm,omitempty" mapstructure:"npm"`
    Run           []string `yaml:"run,omitempty" mapstructure:"run"`
}
func (i ImageConfig) IsEmpty() bool
// OroConfig gains: Image *ImageConfig `yaml:"image,omitempty" mapstructure:"image"`
// The existing top-level Dockerfile field stays, documented as deprecated.
func (c *OroConfig) ImageSettings() ImageConfig   // folds top-level Dockerfile into Dockerfile when image.dockerfile is empty
func GetImageConfig() ImageConfig                  // viper-backed equivalent of ImageSettings
func DeprecatedDockerfileKeyUsed() bool            // top-level `dockerfile` set
func HasCustomLayer() bool                         // !GetImageConfig().IsEmpty()
// GetDockerfile / GetDockerfilePath keep their signatures and now read GetImageConfig().Dockerfile.
```

- [ ] **Step 1: Write failing tests** (table-driven, via `ParseConfig` + `Validate` with a minimal valid config: `oro_version`, one domain, `type: project`):
  - `TestValidateImageEntries`: `apk: [""]` → error containing `image.apk`; `php_extensions: ["redis; rm -rf /"]` → error containing `image.php_extensions`; `npm: ["@playwright/test"]`, `apk: ["php84-pecl-redis=6.1.0-r0"]` → no error; `run: ["a && b | c"]` → no error (run is free-form); `run: [""]` → error.
  - `TestValidateImageDockerfilePath`: `image.dockerfile: /abs` and `../out` → same errors as today's top-level checks, message naming `image.dockerfile`.
  - `TestValidateBothDockerfileKeys`: top-level `dockerfile` + `image.dockerfile` → error naming both keys.
  - `TestImageSettingsFoldsDeprecatedKey`: only top-level `dockerfile: docker/Dockerfile` → `ImageSettings().Dockerfile == "docker/Dockerfile"`.
  - `TestSaveConfigMigratesDockerfile`: `SaveConfig` of a config with only top-level `dockerfile` writes `image:\n    dockerfile:` and no top-level `dockerfile:` line.
  - `TestHasCustomLayer` (viper): empty → false; `image.apk=[x]` → true; top-level `dockerfile` → true.
- [ ] **Step 2: Run** `go test ./internal/config/ -v` — expect FAIL (unknown field `image` from `KnownFields(true)`).
- [ ] **Step 3: Implement** the types and functions above. `SaveConfig` moves `c.Dockerfile` into `c.Image.Dockerfile` (allocating `Image` when nil) and clears `c.Dockerfile` before marshalling. Keep the path validation in one helper shared by both keys.
- [ ] **Step 4: Run** `go test ./internal/config/ -v` — expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 3: Layer Dockerfile renderer

**Files:**
- Create: `internal/docker/layer.go`
- Test: `internal/docker/layer_test.go`

**Interfaces:**
- Consumes: `config.ImageConfig` (Task 2).
- Produces:
```go
// RenderLayerDockerfile returns the Dockerfile the custom layer is built from: the project's
// Dockerfile (nil when none) followed by one RUN per non-empty image.* key. Pure: no I/O.
func RenderLayerDockerfile(projectDockerfile []byte, img config.ImageConfig) []byte
```

- [ ] **Step 1: Write failing tests:**
  - `TestRenderLayerDeclarativeOnly`: `img{Apk:[a b], PhpExtensions:[redis], Npm:[x], Run:["echo 1","echo 2"]}`, nil project → exact output:
```
ARG OROBOX_BASE_IMAGE
FROM ${OROBOX_BASE_IMAGE}
USER root
RUN apk add --no-cache a b
RUN install-php-extensions redis
RUN npm install -g x
RUN echo 1
RUN echo 2
```
  - `TestRenderLayerAppendsToProjectDockerfile`: project `"ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN true"` (no trailing newline) + `Apk:[a]` → project text, `\n`, `USER root\nRUN apk add --no-cache a\n`; the header is **not** repeated.
  - `TestRenderLayerProjectOnly`: project bytes and empty lists → output equals project bytes unchanged.
  - `TestRenderLayerSkipsEmptyKeys`: only `Npm` set → no `apk`/`install-php-extensions` lines.
- [ ] **Step 2: Run** `go test ./internal/docker/ -run TestRenderLayer -v` — expect FAIL.
- [ ] **Step 3: Implement** `RenderLayerDockerfile`. Each `run` entry is its own `RUN` line, in order.
- [ ] **Step 4: Run** — expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 4: Build the rendered layer

**Files:**
- Modify: `internal/docker/customimage.go` (`ProjectImageRefs`, `ensureCustomImage`, `customLayerHash`, `buildCustomImage`)
- Modify: `internal/docker/compose.go` (template data: replace `CustomDockerfile string` with `HasCustomLayer bool` and `LayerSource string`)
- Modify: `templates/docker/docker-compose.yml`, `templates/docker/docker-compose.setup.yml` (`{{- if .HasCustomLayer}}`, comment names `{{.LayerSource}}`)
- Modify: `cmd/root.go` (print the deprecation warning once when `config.DeprecatedDockerfileKeyUsed()`, after a successful `Validate`, not in agent mode stdout — use `utils.PrintWarning`)
- Modify: `docs/configuration.md` (new "Image customization (`image`)" section; Custom Dockerfile section now under `image.dockerfile`, old key marked deprecated)
- Test: `internal/docker/customimage_test.go`, `internal/docker/composeimage_test.go`

**Interfaces:**
- Consumes: `RenderLayerDockerfile` (Task 3), `config.HasCustomLayer`, `config.GetImageConfig`, `config.GetDockerfilePath` (Task 2).
- Produces:
```go
func customLayerHash(contextDir, baseID string, dockerfile []byte) (string, error)
func buildCustomImage(ref, base, contextDir string, dockerfile []byte, hash string) error // runs `docker build -f - <contextDir>` with dockerfile on stdin
// checkExtendsBaseImage is exported as CheckExtendsBaseImage (same signature) for Tasks 12 and 14.
func layerSource() string // "docker/Dockerfile", "image keys", or "docker/Dockerfile + image keys" — used in messages and the compose comment
// Empty build context when no project Dockerfile: filepath.Join(config.GetInternalDir(), "layer-context"), created on demand.
```

- [ ] **Step 1: Write failing tests:**
  - `TestCustomLayerHashIncludesDockerfile`: same dir and baseID, two different dockerfile byte slices → different hashes; identical → identical.
  - `TestProjectImageRefsDeclarativeOnly` (viper `image.apk=[x]`, no dockerfile) → `app` is `CustomImageRef(...)`, not `base`.
  - `TestEnsureCustomImageDeprecatedAliasSameRef`: top-level `dockerfile` only → `ProjectImageRefs()` app ref equals the one with `image.dockerfile` set to the same path.
  - `TestComposeTemplatePullPolicyNever` (in `composeimage_test.go`, render real `docker-compose.yml` with `HasCustomLayer: true`) → contains `pull_policy: never`; with false → does not.
  - Build invocation: extract the arg list into `func customImageBuildArgs(ref, base, contextDir, hash string, noCache bool) []string` and test it contains `-f`, `-`, `--build-arg OROBOX_BASE_IMAGE=<base>`, `--label dev.orobox.custom-layer=<hash>`, context dir last, and `--no-cache --pull` only when `noCache`.
- [ ] **Step 2: Run** `go test ./internal/docker/ -v` — expect FAIL.
- [ ] **Step 3: Implement.** `ensureCustomImage`: return nil when `!config.HasCustomLayer()`; read and `checkExtendsBaseImage` the project Dockerfile only when one is set; render; pick context; hash; compare label; build with `cmd.Stdin = bytes.NewReader(rendered)`. All user-facing messages use `layerSource()`.
- [ ] **Step 4: Run** `go test ./... ` — expect PASS.
- [ ] **Step 5: Docs** as listed in Files.
- [ ] **Step 6: Checkpoint.**

## Phase 2 — php.ini and ports

### Task 5: `php_ini` — parse, render, mount

**Files:**
- Create: `internal/config/phpini.go`
- Modify: `internal/config/config.go` (`OroConfig.PhpIni any \`yaml:"php_ini,omitempty" mapstructure:"php_ini"\``, call `validatePhpIni` from `Validate`)
- Modify: `internal/docker/layer.go` (add `RenderPhpIni`)
- Modify: `internal/docker/compose.go` (`EnsureDockerCompose`: write/remove `zz-project.ini`; template data `PhpIniPath string`)
- Modify: `templates/docker/docker-compose.yml` (`volumes-oro` anchor), `templates/docker/docker-compose.setup.yml` (`volumes-setup` anchor): `{{if .PhpIniPath}}- "{{.PhpIniPath}}:/usr/local/etc/php/conf.d/zz-project.ini:ro"{{end}}`
- Modify: `docs/configuration.md` (new "PHP settings (`php_ini`)" section)
- Test: `internal/config/phpini_test.go`, `internal/docker/layer_test.go`, `internal/docker/compose_test.go`

**Interfaces:**
- Produces:
```go
type PhpIni struct {
    File   string         // project-relative path when php_ini is a string
    Values map[string]any // flat directive -> scalar when php_ini is a map
}
func (c *OroConfig) PhpIniSettings() (PhpIni, error)
// GetPhpIni re-parses viper.ConfigFileUsed() with ParseConfig — NOT viper.Get, which splits on
// "." and lowercases keys — and returns PhpIniSettings(). Zero value when no file or no key.
func GetPhpIni() (PhpIni, error)
func validatePhpIni(raw any) error
// in internal/docker/layer.go:
func RenderPhpIni(values map[string]any) (string, error) // keys sorted, one "key = value" per line
```
- Mount source: map form → absolute `<internalDir>/zz-project.ini`; file form → absolute path of the project file.

- [ ] **Step 1: Write failing tests:**
  - `TestGetPhpIniKeepsDottedKeys`: temp `.orobox.yaml` with `php_ini: {xdebug.log_level: 0, Memory_Limit: 4G}` loaded through viper → `Values` has keys `xdebug.log_level` and `Memory_Limit` exactly.
  - `TestValidatePhpIni`: nested map → error containing `php_ini`; list value → error; absolute path string / `../x` → error; path to missing file → error; existing relative file → ok.
  - `TestRenderPhpIni`: `{"b": true, "a": false, "n": -1, "f": 1.5, "s": "Europe/Rome", "q": "a;b", "w": " x"}` → exactly
```
a = Off
b = On
f = 1.5
n = -1
q = "a;b"
s = Europe/Rome
w = " x"
```
  - `TestRenderPhpIniQuotesSpecialChars`: each of `; = " { } | & ~ ! [ ( ) ^` in a value → value double-quoted (an embedded `"` is escaped as `\"`).
  - `TestEnsureDockerComposeWritesPhpIni` (pattern of existing `compose_test.go` temp-dir tests): map form → `zz-project.ini` exists in internal dir; removing the key and re-running → file removed.
  - Real-template render test: `PhpIniPath: "/x/zz-project.ini"` → both `docker-compose.yml` and `docker-compose.setup.yml` contain `/x/zz-project.ini:/usr/local/etc/php/conf.d/zz-project.ini:ro`; empty → neither contains `zz-project.ini`.
- [ ] **Step 2: Run** `go test ./internal/config/ ./internal/docker/ -v` — expect FAIL.
- [ ] **Step 3: Implement.** `EnsureDockerCompose` counts a written/removed ini as `changed`. On `GetPhpIni` error print a warning and mount nothing (Validate already rejected bad configs).
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

### Task 6: `ports:` map

**Files:**
- Create: `internal/config/ports.go`
- Modify: `internal/config/config.go` (`OroConfig.Ports map[string]int \`yaml:"ports,omitempty" mapstructure:"ports"\``, validation call)
- Modify: `internal/docker/compose.go` (`GetNginxPorts` precedence; template data `Ports map[string]int`)
- Modify: `templates/docker/docker-compose.yml`, `docker-compose.setup.yml` (gotenberg), `docker-compose.test.yml` (db-test): every published host port from `.Ports`, entry omitted when `0`
- Modify: `cmd/up.go` (every printed `localhost:<port>` and the "External Database Connection" port from `config.GetPort`; skip a block's URL line when its port is `0`)
- Modify: `docs/configuration.md` ("Host ports (`ports`)" section with the key table)
- Test: `internal/config/ports_test.go`, `internal/docker/compose_test.go`, `cmd/cmd_test.go` (or the existing up test file)

**Interfaces:**
- Produces:
```go
var DefaultPorts = map[string]int{ /* the 14 keys and defaults from Global Constraints */ }
func validatePorts(p map[string]int) error // unknown key -> error naming it and listing valid keys; <0 or >65535 -> error
func GetPorts() map[string]int             // DefaultPorts overlaid with viper "ports"
func GetPort(key string) int
```
- `GetNginxPorts` precedence: `ports.http/https` → `nginx_http_port/nginx_https_port` → `ORO_NGINX_HTTP(S)_PORT` env → defaults.

- [ ] **Step 1: Write failing tests:**
  - `TestValidatePorts`: `{"dbb": 1}` → error containing `dbb`; `{"db": 70000}` → error; `{"db": 0}` → ok.
  - `TestGetPortsDefaults`: no config → equals `DefaultPorts`.
  - `TestGetNginxPortsPrecedence`: `ports.http=9000` + `nginx_http_port=7000` → `9000`; only `nginx_http_port` → `7000`.
  - Real-template render: default `Ports` → contains `"5432:5432"`, `"6379:6379"`, `"8025:8025"`; `db: 5434` → contains `5434:5432`; `adminer: 0` → adminer service has no `ports:` entry for 8080 target.
  - `up` output test: `ports.adminer=8090` → printed `http://localhost:8090`; `ports.db=5434` → printed `Port: 5434`.
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement.** Use a template helper or precomputed `.Ports` lookups; for `0` wrap the list item (and the `ports:` key when it would be empty) in `{{if}}`.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

## Phase 3 — Override and env

### Task 7: `.env` merge

**Files:**
- Create: `internal/docker/envmerge.go`
- Modify: `internal/docker/compose.go` (`writeEnvFile`: always render the template, then merge the local file when present)
- Modify: `docs/configuration.md` ("Environment files" — merge semantics)
- Test: `internal/docker/envmerge_test.go`

**Interfaces:**
- Produces:
```go
// MergeEnv applies the project's assignments over the rendered template: a key the template
// defines is replaced on its own line; other keys are appended after "# From <source>".
func MergeEnv(template, project []byte, source string) []byte
```

- [ ] **Step 1: Write failing tests:**
  - `TestMergeEnvReplacesInPlace`: template `"# c\nA=1\nB=2\n"`, project `"B=3\n"` → `"# c\nA=1\nB=3\n"`.
  - `TestMergeEnvAppendsNewKeys`: project `"Z=9\n"` → ends with `"\n# From .env\nZ=9\n"`.
  - `TestMergeEnvKeepsReferencesVerbatim`: project `X="${A}/x"` → line copied byte for byte.
  - `TestMergeEnvTolerantParsing`: project `"export K=1\r\nQ=\"a b\"\r\nE=\r\n# note\r\n\r\n"` → template gains `K=1`, `Q="a b"`, `E=` (no `\r`, no comment, `export ` dropped).
  - `TestMergeEnvFullCopyIsIdentity`: project equal to the template → output equals the template.
- [ ] **Step 2: Run** `go test ./internal/docker/ -run TestMergeEnv -v` — expect FAIL.
- [ ] **Step 3: Implement** `MergeEnv`; key = text before the first `=` after trimming an optional `export `; last assignment of a key in the project file wins.
- [ ] **Step 4: Run** `go test ./...` — expect PASS (existing `writeEnvFile` tests updated where they asserted replacement).
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

### Task 8: Override path resolution and analysis

**Files:**
- Create: `internal/composeoverride/resolve.go`, `internal/composeoverride/analyze.go`
- Test: `internal/composeoverride/resolve_test.go`, `internal/composeoverride/analyze_test.go`

**Interfaces:**
- Produces:
```go
package composeoverride

// Resolve rewrites relative host paths in a compose file to absolute ones against baseDir,
// expanding a leading "~" with home. It edits the yaml.v3 node tree, so tags (!override,
// !reset), comments and unknown keys survive. An empty or comments-only document returns
// (nil, nil).
func Resolve(src []byte, baseDir, home string) ([]byte, error)

type ServiceURL struct{ Service, URL string }
type Analysis struct {
    MissingPaths       []string     // absolute bind sources that do not exist on the host
    CoreImageOverrides []string     // core services whose `image` is redefined
    HasBuild           bool         // any service has a `build` key
    URLs               []ServiceURL // services with a dev.orobox.url label, sorted by service
}
func Analyze(resolved []byte, coreServices []string, exists func(string) bool) (Analysis, error)
```

- [ ] **Step 1: Write failing tests** for `Resolve` (baseDir `/proj`, home `/home/u`), one subtest per row of the spec's "Rewritten keys" table, plus:
  - short volume `./a:/b:ro` → `/proj/a:/b:ro`; `~/x:/y` → `/home/u/x:/y`; `named:/data` unchanged; `/abs:/y` unchanged; `${VAR}/x:/y` unchanged.
  - long volume `{type: bind, source: ./a}` → `/proj/a`; `{type: volume, source: v}` unchanged.
  - `build: ./svc` → `/proj/svc`; `build: {context: ., dockerfile: Dockerfile.dev}` → context `/proj`, dockerfile unchanged.
  - `env_file` as string, list, and `[{path: ./e}]`; `extends.file`; `label_file` string and list; `configs.c.file`, `secrets.s.file`; `include: [./a.yaml]` and `include: [{path: ./a.yaml, env_file: ./e, project_directory: ./d}]`.
  - `TestResolvePreservesTags`: `ports: !override ["1:1"]` and `!reset []` survive in the output.
  - `TestResolveEmptyDocument`: `""` and `"# only a comment\n"` → `nil, nil`.
  - `TestResolveInvalidYAML` → error.
- [ ] **Step 2: Write failing tests** for `Analyze`: missing path reported (exists returns false), existing not; `application: {image: x}` → `CoreImageOverrides == ["application"]`; `build:` anywhere → `HasBuild`; labels in map form and list form (`- dev.orobox.url=http://x`) both produce URLs.
- [ ] **Step 3: Run** `go test ./internal/composeoverride/ -v` — expect FAIL.
- [ ] **Step 4: Implement** both functions on `*yaml.Node` traversal (mapping key lookup helper, no struct decoding).
- [ ] **Step 5: Run** — expect PASS.
- [ ] **Step 6: Checkpoint.**

### Task 9: Wire the override into compose

**Files:**
- Modify: `internal/docker/compose.go` (`EnsureDockerCompose` calls `writeComposeOverrides`; `GetBaseComposeArgs` appends resolved files; `RunComposeCommandSilently` and the two other runners at `compose.go:594`/`:663` return `OverrideError()` first; `up` gets `--build` when `OverrideAnalysis().HasBuild`)
- Create: `internal/docker/override.go`
- Modify: `internal/scaffold/` bundle `.gitignore` template `templates/bundle/gitignore.tmpl` (add `/.orobox.compose.local.yaml`)
- Modify: `docs/configuration.md` ("Extending the stack (`.orobox.compose.yaml`)" — files table, relative paths, `!override`/`!reset`, `down -v` and `external: true`, pipeline does not use it)
- Test: `internal/docker/override_test.go`

**Interfaces:**
- Consumes: `composeoverride.Resolve`, `composeoverride.Analyze` (Task 8).
- Produces:
```go
var OverrideFiles = []struct{ Source, Resolved string }{
    {".orobox.compose.yaml", "compose.project.resolved.yaml"},
    {".orobox.compose.local.yaml", "compose.local.resolved.yaml"},
}
var CoreServices = []string{"application", "web", "php-fpm-app", "ws", "consumer", "cron", "volume-init", "web-init"}
func writeComposeOverrides(internalDir, projectDir string) (changed bool, err error)
func OverrideError() error                         // error from the last writeComposeOverrides, nil otherwise
func OverrideAnalysis() composeoverride.Analysis   // merged analysis of both files
```

- [ ] **Step 1: Write failing tests:**
  - `TestWriteComposeOverridesResolves`: project dir with `.orobox.compose.yaml` containing `./x:/y` → internal `compose.project.resolved.yaml` contains `<projectDir>/x:/y`.
  - `TestWriteComposeOverridesSkipsEmpty`: comments-only file → no resolved file, `GetBaseComposeArgs` has no extra `-f`.
  - `TestWriteComposeOverridesRemovesStale`: resolved file exists, source deleted → resolved file removed.
  - `TestGetBaseComposeArgsOverrideOrder`: both files → the two resolved paths are the last two `-f` values, project before local.
  - `TestWriteComposeOverridesInvalidYAML` → `OverrideError()` non-nil and names `.orobox.compose.yaml`; `RunComposeCommandSilently` returns it without running docker (stub via the existing package var pattern).
  - `TestOverrideWarnings`: missing bind path and `web: {image: x}` → warnings printed (capture via `output`/`utils` test helpers used in `compose_test.go`).
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement.** Warnings use `utils.PrintWarning`, one per finding, naming the path or service.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

### Task 10: CLI awareness — `logs [service...]` and URL labels

**Files:**
- Modify: `cmd/logs.go` (`Args: cobra.ArbitraryArgs`; positional services appended after flag services, deduplicated, order kept)
- Modify: `cmd/up.go` ("Project services:" block from `docker.OverrideAnalysis().URLs`, printed only when non-empty)
- Modify: `docs/commands.md` (logs usage; up output)
- Test: `cmd/cmd_test.go` (or the existing logs/up tests)

- [ ] **Step 1: Write failing tests:** `orobox logs minio` → compose args `logs -f minio`; `orobox logs --nginx web minio` → `logs -f web minio`; no args/flags → help printed, no compose call. `up` with an analysis containing `{minio, http://localhost:9001}` → output contains `Project services` and `minio: http://localhost:9001`.
- [ ] **Step 2: Run** `go test ./cmd/ -v` — expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — expect PASS.
- [ ] **Step 5: Checkpoint.**

## Phase 4 — Scaffolding and recipes

### Task 11: Comment-preserving YAML edits

**Files:**
- Create: `internal/yamledit/yamledit.go`
- Test: `internal/yamledit/yamledit_test.go`

**Interfaces:**
- Produces:
```go
package yamledit

type Doc struct{ /* root *yaml.Node */ }
func Parse(src []byte) (*Doc, error)            // empty input -> empty mapping document
func (d *Doc) Bytes() ([]byte, error)           // 2-space indent, comments kept
func (d *Doc) SetScalar(path []string, value string) error          // creates intermediate mappings
func (d *Doc) AppendUnique(path []string, values ...string) error   // sequence; creates it when missing
func (d *Doc) MergeMapping(path []string, fragment *yaml.Node, overwrite bool) (added, skipped []string, err error) // keys of fragment merged into the mapping at path
func (d *Doc) Has(path []string) bool
func (d *Doc) Kind(path []string) yaml.Kind     // 0 when absent
```

- [ ] **Step 1: Write failing tests:** a fixture `.orobox.yaml` with head, line and foot comments → after `SetScalar([image dockerfile], "docker/Dockerfile")` every original comment is still present and `image.dockerfile` exists; `AppendUnique([image php_extensions], "redis", "redis", "xsl")` on `[redis]` → `[redis, xsl]`; `MergeMapping([services], {a:…, b:…}, false)` on existing `a` → `added=[b] skipped=[a]`, `a` unchanged; with `overwrite=true` → `a` replaced; `Parse(nil)` then `SetScalar` → valid YAML.
- [ ] **Step 2: Run** `go test ./internal/yamledit/ -v` — expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 12: `orobox extend image` and `orobox extend compose [--local]`

**Files:**
- Create: `cmd/extend.go`, `templates/extend/Dockerfile.tmpl`, `templates/extend/compose.yaml.tmpl`, `templates/extend/compose.local.yaml.tmpl`
- Create: `internal/scaffold/extend.go` (file writing logic; `cmd/extend.go` stays thin)
- Modify: `docs/commands.md` (new "Extending the environment (`extend`)" section)
- Test: `internal/scaffold/extend_test.go`, `cmd/cmd_test.go`

**Interfaces:**
- Consumes: `yamledit` (Task 11), `scaffold.Templates`.
- Produces:
```go
type Receipt struct{ Path, Action string } // Action: "created" | "updated" | "skipped"
func ExtendImage(projectDir string) ([]Receipt, error)
func ExtendCompose(projectDir string, local bool) ([]Receipt, error)
```
- Output: one line per receipt, `<action> <path>`; in agent mode only these lines (via `output`).

- [ ] **Step 1: Write failing tests:**
  - `TestExtendImageCreates`: empty project with `.orobox.yaml` (with a comment) → `docker/Dockerfile` created, starting with `ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}`; `.orobox.yaml` has `image.dockerfile: docker/Dockerfile` and still has its comment; receipts `created docker/Dockerfile`, `updated .orobox.yaml`.
  - `TestExtendImageNeverOverwrites`: existing `docker/Dockerfile` → content unchanged, receipt `skipped`; `.orobox.yaml` still updated when the key is missing.
  - `TestExtendImageRendersValidLayer`: the created file passes `docker.CheckExtendsBaseImage("docker/Dockerfile", content)` with a nil error.
  - `TestExtendComposeLocalGitignore`: `.gitignore` without the entry → appended `/.orobox.compose.local.yaml` once; running twice → still once; no `.gitignore` → none created.
  - `TestExtendComposeTemplateParses`: created `.orobox.compose.yaml` parses with `yaml.v3` and `composeoverride.Resolve` returns no error.
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement** (`extend` parent command with `image`, `compose` subcommands; `--local` flag on `compose`).
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

### Task 13: Recipes and `orobox extend add`

**Files:**
- Create: `internal/scaffold/recipes.go`
- Create: `templates/recipes/{varnish,selenium,sftp,blackfire}/recipe.yaml`, `templates/recipes/varnish/files/default.vcl`, `templates/recipes/sftp/files/upload/.gitkeep`
- Modify: `cmd/extend.go` (`add` subcommand, `--force`)
- Modify: `docs/commands.md` (recipes table from the spec, D10)
- Test: `internal/scaffold/recipes_test.go`

**Interfaces:**
- Consumes: `yamledit`, `MergeEnv`-style rule "never overwrite a key" (implement locally: append only keys absent from `.env`), `config.ParseConfig`.
- Produces:
```go
type Recipe struct {
    Name, Description, Notes string
    Compose *yaml.Node        // mapping with services / volumes
    Config  *yaml.Node        // optional, mapping merged into .orobox.yaml
    Env     map[string]string // optional
    Files   fs.FS             // optional, copied to docker/<name>/
}
func LoadRecipes(fsys fs.FS) ([]Recipe, error)                     // sorted by name
func AddRecipe(projectDir string, r Recipe, force bool) ([]Receipt, error)
```
- Merge rules: compose `services`/`volumes` via `MergeMapping(overwrite=force)` into `.orobox.compose.yaml` (created when missing); `config` lists via `AppendUnique`, scalars only when absent; `env` keys appended to `<projectDir>/.env` only when absent (file created when missing); `files/` copied never overwriting; recipe with `config.php_ini` + project `php_ini` as a string → error naming the keys to add by hand.

- [ ] **Step 1: Write failing tests:**
  - `TestRecipesLoadAndPinned` (real `templates/recipes` via `os.DirFS("../../templates")`): exactly `blackfire, selenium, sftp, varnish`; every `services.*.image` has a tag that is not `latest`; every recipe has a non-empty description.
  - `TestRecipesComposeValid`: for each recipe, `composeoverride.Resolve` succeeds; skip with `t.Skip` when `docker` is not on PATH, otherwise `docker compose -f <generated stub> -f <fragment> config -q` exits 0 (stub declares `web`, so varnish's `depends_on: web` resolves).
  - `TestAddRecipeRefusesExistingService`: `.orobox.compose.yaml` already has `sftp` → error naming `sftp`; with `force` → replaced, other services untouched.
  - `TestAddRecipeBlackfire`: `.orobox.yaml` gains `image.php_extensions: [blackfire]` (deduplicated if present) and `php_ini.blackfire.agent_socket`; `.env` gains empty `BLACKFIRE_SERVER_ID`/`BLACKFIRE_SERVER_TOKEN` but an existing `BLACKFIRE_SERVER_ID=abc` is kept.
  - `TestAddRecipePhpIniFileConflict`: project `php_ini: docker/php.ini` + blackfire → error containing `blackfire.agent_socket`.
  - `TestAddRecipeCopiesFiles`: varnish → `docker/varnish/default.vcl` created; second run → `skipped`.
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Write the four recipes** per the spec's D10 table (varnish in front of `web` on port 6081 with `dev.orobox.url`; selenium `standalone-chrome` with noVNC on 7900 and `dev.orobox.url`; sftp `atmoz/sftp` on 2222 mounting `./docker/sftp/upload`; blackfire as in the spec example). Notes text as in the spec table.
- [ ] **Step 4: Implement** `LoadRecipes`, `AddRecipe`, and `orobox extend add [recipe...]` (no args → list `name — description`; after adding, print each recipe's notes).
- [ ] **Step 5: Run** `go test ./...` — expect PASS.
- [ ] **Step 6: Docs.**
- [ ] **Step 7: Checkpoint.**

## Phase 5 — Pipeline parity and end-to-end

### Task 14: Deploy pipeline builds the same layer

**Files:**
- Create: `internal/pipeline/layer.go`
- Modify: `internal/pipeline/plan.go` (`Plan` fields), `internal/pipeline/run.go` (`runner.container` base, built once in `Run`)
- Modify: `cmd/deploy.go:94`, `cmd/qa.go:520`, `cmd/test.go:211` (call `pipeline.ApplyProjectLayer` after building the plan; return its error)
- Modify: `docs/deployment.md` (the pipeline runs the project layer and `php_ini`; compose override is dev-only)
- Test: `internal/pipeline/layer_test.go`

**Interfaces:**
- Consumes: `config.OroConfig.ImageSettings`, `config.OroConfig.PhpIniSettings` (Tasks 2, 5), `docker.RenderLayerDockerfile`, `docker.RenderPhpIni` (Tasks 3, 5).
- Produces:
```go
type LayerSpec struct {
    ContextDir string // host dir: the project Dockerfile's dir, or "" for an empty context
    Dockerfile []byte // rendered
    BaseImage  string // plan.Image before the layer
}
// Plan gains: Layer *LayerSpec; PhpIni string (rendered ini content, "" when none)
func ApplyProjectLayer(p *Plan, conf *config.OroConfig, hostDir string) error
```
- Runner: when `plan.Layer != nil`, the base container is
  `dir.WithNewFile(".orobox.layer.Dockerfile", string(Dockerfile)).DockerBuild(dagger.DirectoryDockerBuildOpts{Dockerfile: ".orobox.layer.Dockerfile", BuildArgs: []dagger.BuildArg{{Name: config.DockerfileBaseImageArg, Value: BaseImage}}})`
  where `dir` is `client.Host().Directory(ContextDir)` or `client.Directory()` when empty; otherwise `client.Container().From(plan.Image)` as today. When `plan.PhpIni != ""`, `WithNewFile("/usr/local/etc/php/conf.d/zz-project.ini", PhpIni)` is applied in `runner.container` after `WithUser("root")`.

- [ ] **Step 1: Write failing tests** (no Dagger engine needed — they test `ApplyProjectLayer` and a pure helper):
  - `TestApplyProjectLayerNone`: empty `image` and `php_ini` → `Layer == nil`, `PhpIni == ""`, `Image` unchanged.
  - `TestApplyProjectLayerDeclarative`: `image.php_extensions: [redis]` → `Layer.Dockerfile` contains `RUN install-php-extensions redis`, `Layer.BaseImage` equals the published tag, `ContextDir == ""`.
  - `TestApplyProjectLayerProjectDockerfile`: `image.dockerfile: docker/Dockerfile` in a temp hostDir → `ContextDir == <hostDir>/docker`; a Dockerfile whose final `FROM` is not the base arg → error (reuse `docker.CheckExtendsBaseImage`, exported in Task 4).
  - `TestApplyProjectLayerPhpIni`: map form → `PhpIni` equals `docker.RenderPhpIni` output; file form → file contents.
  - `TestApplyProjectLayerDeprecatedAlias`: top-level `dockerfile` only → same result as `image.dockerfile`.
- [ ] **Step 2: Run** `go test ./internal/pipeline/ -run TestApplyProjectLayer -v` — expect FAIL.
- [ ] **Step 3: Implement** `ApplyProjectLayer` and the runner change; call sites in the three commands.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Docs.**
- [ ] **Step 6: Checkpoint.**

### Task 15: End-to-end coverage

**Files:**
- Create: `e2e/fixtures/customization.orobox.yaml`, `e2e/fixtures/customization.compose.yaml`
- Modify: `e2e/e2e_test.go` (new test, same harness/build tags as existing ones)
- Modify: `CHANGELOG.md` (Unreleased: image keys, php_ini, ports, compose override, `.env` merge, `logs <service>`, `extend`, recipes, pipeline parity, deprecated top-level `dockerfile`)

- [ ] **Step 1: Write the e2e test** `TestE2ECustomization`: project fixture with `image.php_extensions: [redis]`, `image.apk: [poppler-utils]`, `php_ini: {memory_limit: 3G}`, `ports: {db: 5440}` and an override adding `whoami` (`traefik/whoami:<pinned>`, `ports: ["8099:80"]`, `dev.orobox.url`). After `orobox init` + `orobox up`, assert: `orobox run`/exec `php -m` lists `redis`; `php -r 'echo ini_get("memory_limit");'` prints `3G`; `which pdftotext` succeeds; `docker compose ps whoami` is running; `up` output contains `whoami: http://localhost:8099`; host port 5440 accepts TCP.
- [ ] **Step 2: Run** `make e2e` (long; GitHub matrix or local) — expect PASS.
- [ ] **Step 3: CHANGELOG.**
- [ ] **Step 4: Checkpoint** — final list of every file changed across the plan for the maintainer's commit.
