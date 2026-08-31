# init: prompt for oro:install on existing project checkout

Date: 2026-06-30

## Problem

In `--type=project` mode the user's repository is bind-mounted onto `OroRoot`.
When the checkout already contains a `composer.json` (the OroCommerce
application is already present), `init` still unconditionally runs step 5
(`oro:install`), which resets the database. Re-running `init` on an existing
project is therefore destructive with no opt-out.

## Goal

When `init` runs in project mode and `composer.json` is already present in the
source root, ask whether to run `oro:install` instead of always running it.
Default to **not** installing (non-destructive). Provide a flag to force it.

## Scope

- Applies **only** to `--type=project`. Bundle behavior is unchanged
  (`oro:install` always runs).
- Trigger is the presence of `composer.json` in the container source root
  (the existing check at `cmd/init.go` ~line 145).

## Design

### Flag

Add a boolean flag to `initCmd`:

```
--force-install   Force oro:install even if the project already has composer.json (default false)
```

Backed by a package var `forceInstall bool`.

### Control flow in `performInstallation` (cmd/init.go)

Introduce `runOroInstall := true` before the composer.json check.

At the existing composer.json check:

- **composer.json missing** (fresh scaffold path): leave `runOroInstall = true`.
  A freshly scaffolded app must be installed.
- **composer.json present**:
  - If `strategy.BindWholeRepo()` is true (project):
    - If `forceInstall` is set → keep `runOroInstall = true`.
    - Else → `runOroInstall = utils.AskYesNo(reader, "OroCommerce already present (composer.json found). Run oro:install? This resets the database", false)`.
  - Else (bundle) → leave `runOroInstall = true` (unchanged behavior).
  - Existing vendor-check / `composer install` logic is unchanged — dependencies
    are still resolved regardless of the install decision.

`reader` is a `bufio.NewReader(stdin)` created where needed (reusing the
package-level `stdin io.Reader = os.Stdin` seam used elsewhere in this file).

### Non-interactive behavior

Every `Ask*` function blocks in `ReadString` until a newline arrives, so it
yields its default only at EOF. A CI or daemon process that inherits an open
pipe on stdin never reaches EOF and would hang there forever.
`utils.SkipPrompts(stdin)` identifies exactly that case: an `*os.File` that is
not a terminal (`term.IsTerminal` on its descriptor). A terminal is read
normally, and so is an in-memory reader — the test seam — because it is already
complete and reaches EOF on its own.

Two places consult it, because both run before anything else in `init` and
either could hang:

- The `oro:install` question takes its safe default (`false`, skip) without
  reading. `--force-install` is the escape hatch for scripted reinstall.
- `generateConfig()`'s wizard reads from an exhausted reader instead of stdin,
  so every question falls straight through to its default. That is the same
  outcome a closed stdin already produced for CI and the e2e harness, which is
  how those runs generate a configuration today; `-t`, `-v` and `-n` remain the
  way to steer them off the defaults.

### Gating step 5

Wrap the volume-setup-for-install + `install` block (current ~lines 219–234):

```go
if runOroInstall {
    // volume-setup + run install (current code)
} else {
    utils.PrintInfo("Skipping OroCommerce installation (existing project preserved).")
}
```

The `down --remove-orphans`, service start, volume-init, scaffold, and
composer-install steps run as before regardless of `runOroInstall`.

## Testing

- `init_test.go` continues to pass.
- Add a test asserting the `--force-install` flag is registered on `initCmd`
  and binds to `forceInstall`.
- Add a `utils.SkipPrompts` test covering the three shapes stdin takes: an
  in-memory reader (read normally), an `os.Pipe` read end and `os.DevNull`
  (both skipped).
- Add a `generateConfig` test that points `stdin` at the read end of a pipe
  nothing writes to or closes, runs the wizard in a goroutine and fails if it
  has not returned within 10s. On timeout the working directory is left in the
  temp directory on purpose: the blocked goroutine outlives the test and would
  otherwise drop `.orobox.yaml` into the repository.
- `performInstallation` requires Docker and remains integration-level (not unit
  tested here).

## Out of scope

- Detecting whether the database is already installed.
- Any change to bundle install flow.
- Any change to the fresh-scaffold path.
