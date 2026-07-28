# Design: `type: project` re-integration

**Date:** 2026-06-25
**Status:** Approved — ready for implementation plan

## Background

Orobox is a Go CLI that provisions isolated local OroCommerce dev environments via
Docker Compose. The `main` branch currently supports only `type: bundle`.

A previous branch, `feat-project-setup`, implemented `type: project`, but it did so
with `{{if eq .Type "project"}}` conditionals scattered across the Docker Compose
templates. Since that branch diverged (merge-base `641d897`), `main` was heavily
refactored: it deleted all template type-branching, removed the supporting Go
scaffolding (vendor-host sync helpers, setup-profile flow internals), introduced the
`{{.OroRootDir}}` template variable, `profiles: [setup]`, and stale-cache clearing.

Re-merging the branch is therefore not viable — the feature must be re-implemented on
top of `main`'s current structure, in a way that is maintainable and ready to absorb a
third type (`demo`) later.

## Goal

Add `type: project` support to `main` such that:

- The bundle-vs-project divergence lives in **one Go file**, not scattered across YAML.
- Compose templates branch on **semantic capability flags**, never on type-string
  equality (`{{if eq .Type "x"}}`).
- Adding `type: demo` later is a single new strategy file with no template churn.

## Core semantic difference

| Aspect | `bundle` | `project` |
|---|---|---|
| What the user's code is | one bundle grafted onto a prebuilt Oro app | the whole checkout **is** the Oro app |
| Container source mount | named volume `oro_app` + bundle submounted at `OroRoot/bundles/<ns>` + `vendor-oro` mount | host repo bind-mounted directly onto `OroRoot` (`cached`) |
| Composer wiring | `composer config repositories.bundle` + `composer require <pkg>:@dev` | `composer install` (vendor from the repo's own `composer.lock`) |
| Vendor sync to host | yes (`/vendor-host`) | no |
| Image tag | `...-bundle-latest` | `...-project-latest` |
| Source root in container | `OroRoot/bundles/<ns>` | `OroRoot` |
| Oro installer | `install ${ORO_INSTALL_OPTIONS}` (existing `install` service) | identical — unchanged |

`OroRoot` = `config.OroRootDir` = `/var/www/oro`.

**Bootstrap behavior:** if the project checkout has no `composer.json` yet, `init`
scaffolds a full OroCommerce application into it (clone the `orocommerce-application`
skeleton + `composer install`), then bind-mounts it onto `OroRoot`. If `composer.json`
is already present, the existing checkout is used as-is. This mirrors the bundle clone
path; the only difference is the destination (bind-mounted checkout vs `oro_app` volume).

## Scope

- **In scope:** `type: project` end to end (config, init, compose generation, setup
  flow, QA/test/xdebug/run working-dir resolution, tests, README).
- **Demo-aware, not demo:** the strategy abstraction must make a future `demo` type a
  single new file. `demo` itself is NOT implemented here and stays parked on
  `feat-demo-setup`.
- **Out of scope:** any unrelated refactor of `main`.

## Architecture

### 1. Install-type strategy — `internal/config/installtype.go` (new)

A single file owns every behavioral difference between install types.

```go
// InstallType encapsulates everything that differs between Orobox install types.
type InstallType interface {
    Name() string                  // "bundle" | "project"
    ImageSuffix() string           // image tag component: ...-<suffix>-latest
    SourceRootContainer() string   // bundle: OroRoot/bundles/<ns>; project: OroRoot
    RunsComposerRequire() bool     // bundle only
    RunsComposerInstall() bool     // project only (vendor from repo's lock)
    SyncsVendorToHost() bool       // bundle only
    RequiresBundleNamespace() bool // bundle only — gates init prompts + validation
    WarnOnMissingComposerJSON() bool
    BindWholeRepo() bool           // project: bind repo onto OroRoot; bundle: false
}

// InstallTypeFor resolves a type name to its strategy, or an error for unknown types.
func InstallTypeFor(name string) (InstallType, error)
```

- `bundleType` and `projectType` structs implement the interface.
- `InstallTypeProject = "project"` const restored alongside `InstallTypeBundle`.
- Unknown type → error surfaced through `OroConfig.Validate()`.
- `SourceRootContainer()` for bundle reuses the existing namespace logic
  (`OroRoot/bundles/` + namespace path); for project returns `OroRootDir`.

### 2. config.go

- Restore `InstallTypeProject` const.
- Add `GetSourceRootContainerPath()` that resolves the active type's strategy and
  returns `SourceRootContainer()`. Existing `GetBundleRootContainerPath()` stays as the
  bundle-specific helper (now used only where bundle semantics are explicitly required).
- `Validate()` resolves the type via `InstallTypeFor` (rejects unknown types) and only
  requires `namespace`/`class` when `RequiresBundleNamespace()` is true.

### 3. compose.go

- Extend the template `data` struct (around compose.go:185) with strategy-derived
  fields: `ImageSuffix`, `SourceRootContainer`, `BindWholeRepo`, `RunsComposerRequire`,
  `RunsComposerInstall`, `SyncsVendorToHost`.
- Populate them from `InstallTypeFor(viper.GetString("type"))`.
- Replace hardcoded `-bundle-latest` with `{{.ImageSuffix}}`.
- Gate the "composer.json not found" warning (compose.go:308) on
  `WarnOnMissingComposerJSON()` instead of `Type == InstallTypeBundle`.

### 4. Templates — `docker-compose.setup.yml`, `docker-compose.yml`

Templates branch on capability flags, not type strings.

- **Mounts** (both `volumes-setup` anchor and the runtime compose volumes):
  ```
  {{if .BindWholeRepo -}}
  - "{{.BundlePath}}:{{.OroRootDir}}:cached"
  {{- else -}}
  - "oro_app:{{.OroRootDir}}:delegated"
  - "{{.BundlePath}}:{{.BundleRootContainerPath}}:cached"
  - "{{.BundlePath}}/vendor-oro:{{.OroRootDir}}/vendor:delegated"
  {{- end}}
  - "cache:{{.OroRootDir}}/var/cache:delegated"
  - "public_storage:{{.OroRootDir}}/public/media:delegated"
  - "private_storage:{{.OroRootDir}}/var/data:delegated"
  ```
  (The shared `cache`/storage mounts apply to both types.)
- **composer-require block** wrapped in `{{if .RunsComposerRequire}}`.
- **vendor-host populate + sync** wrapped in `{{if .SyncsVendorToHost}}`.
- **New composer-install block** in `volume-setup`, wrapped in `{{if .RunsComposerInstall}}`:
  `COMPOSER_ALLOW_SUPERUSER=1 composer install --no-interaction --no-scripts` so the
  project's vendor is materialized before the `install` service runs.
- Keep `{{.OroRootDir}}` variable, `profiles: [setup]`, and stale-cache clearing.
- The `install` service (Oro installer) is unchanged and shared by both types.

### 5. init.go

- Restore the interactive "Installation type" prompt (`bundle` / `project`), default
  `bundle`.
- Resolve the chosen type's strategy; only ask the bundle class/namespace questions when
  `RequiresBundleNamespace()` is true.

### 6. Working-directory consumers

Swap `GetBundleRootContainerPath()` → `GetSourceRootContainerPath()` in the commands
that run inside the container so they target the correct root per type:

- `cmd/qa_init.go`, `cmd/qa.go` — QA tools run against the whole app (project) or the
  bundle subdir (bundle).
- `cmd/test_init.go`, `cmd/test.go` — test working dir.
- `cmd/xdebug.go` — paths.
- `cmd/run.go` — custom-command working dir.

Each call site is reviewed individually; only those that mean "the user's source root"
are swapped. Anything that genuinely needs the bundle path keeps the bundle helper.

### 7. Tests

- `internal/config/installtype_test.go` — each strategy's flags + `SourceRootContainer`.
- `config_test.go` — parse a `type: project` config; `Validate()` accepts project
  without bundle namespace, rejects unknown types.
- `compose_test.go` — golden render of `docker-compose.setup.yml` and
  `docker-compose.yml` for both `bundle` and `project`, asserting the correct mounts,
  image suffix, and which composer blocks appear.
- `init_test.go` — install-type prompt routing (project skips bundle-namespace prompts).

### 8. README

Document `type: project`: what it does (whole repo as the Oro app), the bind-mount
behavior, and that `composer install` + `oro:install` run during setup.

## Conflict / risk notes

- `main` deleted the project template branching and the vendor-host scaffolding; this
  design re-introduces the project paths on `main`'s new template shape (using
  `OroRootDir` and capability flags), so it is new code, not a cherry-pick.
- `GetDatabaseCredentials*` and other helpers were re-shaped on `main`; the project
  feature does not depend on their old signatures, so no collision there.
- `composer install` for project assumes the repo ships a `composer.lock`. If absent,
  composer resolves from `composer.json` — acceptable; document the expectation.

## Success criteria

- `orobox init` offers bundle/project; choosing project writes a valid `type: project`
  `.orobox.yaml` without bundle namespace.
- `orobox up` for a project bind-mounts the repo onto `/var/www/oro`, runs
  `composer install` then the Oro installer, and brings the app up.
- `orobox init --type=project` in an empty checkout scaffolds the OroCommerce
  application into it, then proceeds as above.
- Bundle behavior is byte-for-byte unchanged (golden compose tests prove it).
- All new behavior is unit-tested; `make` build + lint + tests pass.
- Adding `demo` later requires a new strategy file and no template edits.
