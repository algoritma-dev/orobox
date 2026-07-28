# Design: `create` command — separate scaffolding from `init`

**Date:** 2026-07-24
**Status:** Implemented

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

### `orobox create bundle <ClassName>`

- Generates a minimal, valid Oro bundle skeleton from embedded templates
  (`templates/bundle/*`) into a new directory. **No network at all.**
- Argument is the bundle class, accepted either short (`AcmeFooBundle`) or
  fully-qualified (`Acme\FooBundle\AcmeFooBundle`) — matching the style `init`'s existing
  prompt already uses.
- Flags: `--namespace` (PHP namespace override), `--package` (composer package name
  override), `--dir` (target dir; default = short class name).
- Refuse if the target dir exists and is non-empty.
- Stops.

**Argument derivation (bundle):**

| Input form | ClassName | Namespace |
|---|---|---|
| `Acme\FooBundle\AcmeFooBundle` (contains `\`) | `AcmeFooBundle` | `Acme\FooBundle` |
| `AcmeFooBundle` (short) + `--namespace Acme\FooBundle` | `AcmeFooBundle` | `Acme\FooBundle` |
| `AcmeFooBundle` (short), no flag | `AcmeFooBundle` | `AcmeFooBundle` (top-level; a warning suggests passing a namespace) |

- `Prefix` = ClassName with a trailing `Bundle` stripped (`AcmeFoo`).
- `Alias` = snake_case of `Prefix` (`acme_foo`), used as the DI config tree name.
- `PackageName` = `--package` value, else a kebab derived from the namespace
  (fallback `orobox/<kebab-classname>`).

**Generated skeleton (`<dir>/`):**

```
<ClassName>.php
DependencyInjection/<Prefix>Extension.php
DependencyInjection/Configuration.php
Resources/config/services.yml
Resources/config/oro/bundles.yml
composer.json
.gitignore
```

Template contents (Go `text/template`, vars `Namespace`, `ClassName`, `Prefix`, `Alias`,
`PackageName`):

- `<ClassName>.php` — class extending `Symfony\Component\HttpKernel\Bundle\Bundle`.
- `<Prefix>Extension.php` — `Extension` loading `services.yml` via `YamlFileLoader`.
- `Configuration.php` — `ConfigurationInterface` returning `new TreeBuilder('{{.Alias}}')`.
- `Resources/config/services.yml` — empty `services:` block.
- `Resources/config/oro/bundles.yml` — `bundles: [ { name: {{.Namespace}}\{{.ClassName}}, priority: 30 } ]`.
- `composer.json` — `type: symfony-bundle`, PSR-4 autoload `{{.Namespace}}\\: ""`,
  `require.php >=8.1`, `name: {{.PackageName}}`.
- `.gitignore` — ignore `/vendor/` and `/vendor-oro/`.

Exact PHP bodies are conventional and may be tuned during implementation; the file set
and the template variables are the contract.

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
  `create bundle` subcommands (flag wiring, arg validation, non-empty-dir guard).
- **`internal/scaffold/scaffold.go`** (new) — testable core:
  - `ScaffoldBundle(destDir, opts)` — renders embedded bundle templates.
  - `ScaffoldProject(destDir, version)` — resolves tag, clones, strips `.git`.
  - Bundle-class parsing/derivation helpers (`ClassName`, `Namespace`, `Prefix`,
    `Alias`, `PackageName`).
- **`internal/scaffold/templates.go`** + **`templates/bundle/*.tmpl`** (new) — embedded
  skeleton templates. Wired in `main.go` alongside `docker.Templates` (the existing
  `//go:embed all:templates/*` already covers `templates/bundle/`).
- **`cmd/init.go`** — remove folder-creation block and the `-b` flag; run in cwd.
- **`cmd/root.go`** — exempt `create` and its subcommands from the `ConfigError`
  `PersistentPreRun` gate (no config exists at create time). Since subcommand
  `cmd.Name()` is the leaf (`project`/`bundle`), gate on the command chain (e.g.
  `cmd.Parent() != nil && cmd.Parent().Name() == "create"`, alongside the existing
  `init`/`self-update`/`internal-gen-docker` exemptions).
- **`README.md`** — document the new `create` → `init` workflow.

## Testing

- **`internal/scaffold/scaffold_test.go`**:
  - Bundle-class derivation table (short, FQ, `--namespace`) → expected ClassName /
    Namespace / Prefix / Alias / PackageName.
  - `ScaffoldBundle` into a temp dir: assert every expected file exists and that rendered
    PHP/YAML/JSON contains the namespace, class, alias, and package name; `composer.json`
    parses as valid JSON; `bundles.yml` parses as valid YAML.
  - Non-empty-dir guard returns an error and writes nothing.
- **`cmd/*_test.go`**:
  - `create bundle` end-to-end into a temp dir (no network) via the existing command
    test harness.
  - `create project` argument/version resolution and the non-empty-dir guard **without**
    performing a real clone (inject the clone step or guard it behind an interface so the
    network call is not exercised in CI).
  - `init` no longer accepts `-b` and operates in cwd (adjust existing init tests).
- `make` build + lint + full test suite pass.

## Out of scope

- `create` writing `.orobox.yaml` (config stays with `init`, per decision).
- Any change to `init`'s install internals beyond removing folder creation.
- `type: demo`.

## Success criteria

- `orobox create project foo` produces `foo/` containing a clean OroCommerce application
  checkout (no `.git`), no config, no containers started.
- `orobox create bundle AcmeFooBundle` produces `AcmeFooBundle/` with a valid, loadable
  Oro bundle skeleton and nothing else.
- Both `create` subcommands refuse a non-empty target dir and exit non-zero.
- `orobox init` run inside a created dir generates config, Docker files, and completes
  install — with no `-b` flag and no folder creation.
- Bundle and project install paths through `init` are otherwise unchanged.
- New scaffolding logic is unit-tested and the full `make` pipeline passes.
