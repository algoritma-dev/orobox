# Design: `create` command — separate scaffolding from `init`

**Date:** 2026-07-24
**Status:** Implemented
**Revised:** 2026-08-31 — bundle placement is driven by the project's PSR-4 map (see
"Where a bundle lands" below), which replaces the original `--namespace`/`--dir` flags.

## Background

Today `orobox init` does everything at once: it creates the target folder
(`--bundle-path`/`-b`), generates `.orobox.yaml` interactively, generates the Docker
Compose files, and runs the heavy install (clone the Oro app inside Docker, `composer
install`, `composer require` for bundles, `oro:install`).

There is no *local* source-tree scaffolding step. For a **project** the Oro app is
git-cloned inside the `application` container during `init`; for a **bundle** the user
must bring their own PHP code and `init` only wires it in via `composer require`.

## Goal

Split "creating the thing you own" from "provisioning the environment":

- **`orobox create project <name>`** and **`orobox create bundle <ClassName>`** lay the
  source tree onto disk, then **stop**. No config, no Docker, no install.
- **`orobox init`** becomes the single provisioning step: config + Docker + install. It
  no longer creates the target folder.

## Guiding principle

> `create` lays down the source tree you own and will version-control, then stops.
> `init` provisions the running environment around it (config + Docker + Oro install).

This one principle resolves both types consistently: `create` always produces exactly
"the code you commit and edit," fast and (near-)offline. `init` stays the one place that
touches config, Docker, and private-credential operations.

## Commands

### `orobox create project <name>`

- Host-side `git clone --depth 1 -b <tag> https://github.com/oroinc/orocommerce-application.git <name>`.
  This is the standard way an Oro project starts and the base repo is **public**, so
  **no credentials are required** at create time.
- `--oro-version` flag (default `6.1`). Resolved to the latest matching tag via the
  existing `utils.GetLatestTag` (already host-side `git ls-remote`).
- After clone, remove the cloned `.git` directory so the user starts with fresh history.
- Refuse if `<name>` exists and is non-empty.
- Stops. The private `composer install` (which needs credentials) and `oro:install`
  remain in `init`.

### `orobox create bundle <namespace>`

- Generates a minimal, valid Oro bundle skeleton from embedded templates
  (`templates/bundle/*`). **No network at all.**
- The argument is the bundle **namespace** (`Acme\Bundle\FooBundle`), because that is the
  one input that decides both where the bundle lands and what its class is called. A
  fully-qualified class (`Acme\Bundle\FooBundle\AcmeFooBundle`) is accepted too.
- Flags: `--path` (target dir), `--standalone` (force an own composer package), `--class`
  (bundle class override), `--package` (composer package name override).
- Refuse if the target dir exists and is non-empty.
- Stops.

**Where a bundle lands.** An OroCommerce application's own `composer.json` autoloads
`"": "src/"`. That rule is the placement rule: `create bundle` reads the PSR-4 map in the
`composer.json` of the current directory and puts the namespace where the project already
autoloads it.

| Situation | Result |
|---|---|
| Inside an Oro checkout (`"": "src/"`) | `src/Acme/Bundle/FooBundle/`, no own `composer.json` |
| A longer prefix also matches (`Acme\: lib/`) | `lib/Bundle/FooBundle/` — PSR-4's longest-prefix rule |
| No `composer.json`, or no prefix covers the namespace | standalone package in `AcmeFooBundle/`, with its own `composer.json` and `.gitignore` |
| `--standalone` | standalone, whatever the map says |
| `--path X` | `X`, keeping whichever of the two shapes applied |

A prefix mapped to the package root (`"Acme\Bundle\FooBundle\": ""`, the shape a *bundle*
checkout uses) cannot host a namespace subtree without colliding with the package's own
files, so it is skipped and the next prefix is tried. That is what makes `create bundle`
inside a bundle checkout fall back to standalone rather than writing into the checkout root.

The in-project shape deliberately omits `composer.json`: the project's autoloader already
covers the tree, and a second `composer.json` there would make composer treat the directory
as a nested package. Oro's kernel still discovers the bundle, through the
`Resources/config/oro/bundles.yml` the skeleton always writes.

**Name derivation:**

| Input | ClassName | Namespace |
|---|---|---|
| `Acme\Bundle\FooBundle` | `AcmeFooBundle` | `Acme\Bundle\FooBundle` |
| `Acme\FooBundle` | `AcmeFooBundle` | `Acme\FooBundle` |
| `Acme\Bundle\AcmeFooBundle` | `AcmeFooBundle` (not doubled) | `Acme\Bundle\AcmeFooBundle` |
| `Acme\Bundle\FooBundle\AcmeFooBundle` | `AcmeFooBundle` | `Acme\Bundle\FooBundle` |
| `FooBundle` | `FooBundle` | `FooBundle` |

The class form is told apart from the namespace form by the segment before the last one: in
the class form it is itself a `*Bundle` namespace segment, while in the namespace form it is
either a vendor or Oro's literal `Bundle` separator.

- `Prefix` = ClassName with a trailing `Bundle` stripped (`AcmeFoo`).
- `Alias` = snake_case of `Prefix` (`acme_foo`), used as the DI config tree name. It has to
  equal what Symfony derives from the Extension class name, or the container fails to build.
- `PackageName` = `--package` value, else `<kebab first segment>/<kebab last segment>`
  (fallback `orobox/<kebab-classname>`).

**Generated skeleton (`<dir>/`):**

```
<ClassName>.php
DependencyInjection/<Prefix>Extension.php
DependencyInjection/Configuration.php
Resources/config/services.yml
Resources/config/oro/bundles.yml
composer.json    (standalone only)
.gitignore       (standalone only)
```

Template contents (Go `text/template` with `[[ ]]` delimiters, the same choice
`internal/pipeline/templates.go` and the rest of `internal/scaffold` made; vars `Namespace`,
`ClassName`, `Prefix`, `Alias`, `PackageName`):

- `<ClassName>.php` — class extending `Symfony\Component\HttpKernel\Bundle\Bundle`.
- `<Prefix>Extension.php` — `Extension` loading `services.yml` via `YamlFileLoader`.
- `Configuration.php` — `ConfigurationInterface` returning `new TreeBuilder('[[.Alias]]')`.
- `Resources/config/services.yml` — empty `services:` block.
- `Resources/config/oro/bundles.yml` — `bundles: [ { name: <FQCN>, priority: 30 } ]`.
- `composer.json` — `type: symfony-bundle`, PSR-4 autoload `<Namespace>\: ""`,
  `require.php >=8.1`, `name: <PackageName>`.
- `.gitignore` — ignore `/vendor/` and `/vendor-oro/`.

## Interaction with `init` (slimmed)

`init` keeps config generation + install; it **drops folder creation**:

- Remove the `--bundle-path`/`-b` flag and the `filepath.Abs` + `os.MkdirAll` + `os.Chdir`
  block at the top of `initCmd.Run`. `init` now runs in the current working directory
  (the dir `create` produced, or any pre-existing project/bundle dir).
- `generateConfig()`, SSL, hosts check, `EnsureDockerCompose()`, and
  `performInstallation()` are unchanged.
- The `bundlePath` package variable is replaced by the working directory. Any remaining
  reference is repointed to `os.Getwd()`-derived paths (verified during implementation).

**No new coupling needed.** `init`'s existing install logic already does the right thing
after `create`:

- **project**: `create` left a `composer.json` at the root, so `init`'s
  `BindWholeRepo() && composer.json present` path is taken — it asks whether to run
  `oro:install` (default no) instead of re-cloning. `init`'s in-container clone remains
  as a fallback for a bare dir with no `composer.json`.
- **bundle**: `create` generated the bundle class, so `init`'s `generateConfig` →
  `FindPhpClass(".", ...)` locates it, and `composer require ...:@dev` wires it in as
  today.

## Files touched

- **`cmd/create.go`** (new) — parent `create` cobra command plus `create project` and
  `create bundle` subcommands (flag wiring, arg validation, placement reporting).
- **`internal/scaffold/bundle.go`** (new) — `ParseBundleArg` (name derivation),
  `BundleArtifacts`/`Bundle` (rendering through the package's existing `Artifact`/`WriteAll`
  machinery), `ResolveBundlePlacement` (where and what shape).
- **`internal/scaffold/psr4.go`** (new) — `ResolvePsr4Dir`: read a project's composer.json
  PSR-4 map and map a namespace onto a directory.
- **`internal/scaffold/project.go`** (new) — resolve tag, clone, strip `.git`.
- **`internal/scaffold/scaffold.go`** — the shared renderer gains an `esc` template function
  so a PHP namespace can be embedded in composer.json's JSON strings.
- **`templates/bundle/*.tmpl`** (new) — the skeleton templates, wired in `main.go` alongside
  `docker.Templates` (the existing `//go:embed all:templates/*` already covers them).
- **`cmd/init.go`** — remove the folder-creation block and the `-b` flag; run in cwd.
- **`cmd/root.go`** — `isConfigExempt` exempts `create` and its subcommands from the
  `ConfigError` gate, since no config exists at create time.
- **`README.md`**, **`e2e/README.md`** — document the `create` → `init` workflow and the e2e
  coverage.

## Testing

- **`internal/scaffold/bundle_test.go`** — the name-derivation table (namespace form, class
  form, vendor-doubling, overrides, malformed input); the standalone and in-project file
  sets; the non-empty-dir guard; `ResolveBundlePlacement` across all four situations.
- **`internal/scaffold/psr4_test.go`** — the PSR-4 map: root prefix, longest prefix wins, a
  non-matching prefix skipped, segment-boundary matching, list values, a package-root map
  falling through, and the difference between "no PSR-4 root" (the standalone case) and a
  composer.json that is broken (an error the user has to see).
- **`internal/scaffold/realtemplates_test.go`** — renders the templates actually shipped and
  asserts the generated composer.json and bundles.yml parse and hold the right values.
- **`cmd/create_test.go`** — the command wiring against a real working directory: PSR-4
  placement, longest prefix, standalone fallback, `--standalone`, `--path`, `--class`,
  `--package`.
- **`e2e/create_test.go`** (`//go:build e2e`) — the two subcommands host-side, no Docker:
  the standalone skeleton and its composer.json, the non-empty-target refusals, PSR-4
  placement, `--path`, and a real clone of the OroCommerce application that pins the
  `"": "src/"` premise the placement rule depends on.
- **`e2e/e2e_test.go`** — the matrix green path scaffolds a bundle into the real project
  checkout before `qa` and `test`, then proves the application loads it: `console
  cache:clear` succeeds and the bundle's extension alias appears in `console debug:config`.
- `make` build + lint + full test suite pass.

## Out of scope

- `create` writing `.orobox.yaml` (config stays with `init`, per decision).
- Any change to `init`'s install internals beyond removing folder creation.
- `type: demo`.

## Success criteria

- `orobox create project foo` produces `foo/` containing a clean OroCommerce application
  checkout (no `.git`), no config, no containers started.
- `orobox create bundle 'Acme\Bundle\FooBundle'` inside a project checkout produces
  `src/Acme/Bundle/FooBundle/` — a bundle the application autoloads and Oro's kernel
  registers — and outside one produces a standalone `AcmeFooBundle/` package.
- Both `create` subcommands refuse a non-empty target dir and exit non-zero.
- `orobox init` run inside a created dir generates config, Docker files, and completes
  install — with no `-b` flag and no folder creation.
- Bundle and project install paths through `init` are otherwise unchanged.
- New scaffolding logic is unit-tested and the full `make` pipeline passes.
