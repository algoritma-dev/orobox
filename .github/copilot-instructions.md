# Copilot instructions for orobox

Orobox is a Go CLI (`main.go`, `cmd/`, `internal/`) that generates and drives a Docker Compose
based local development environment for OroCommerce (a Symfony-based B2B e-commerce platform).
It has three install modes — `bundle`, `project`, `demo` — selected via `.orobox.yaml`.

See the full docs before making non-trivial changes:
- [README.md](../README.md) — what the tool does, install types, quick start.
- [docs/](../docs/) — configuration, commands, QA toolchain, deployment, debugging reference.
- [docs/agents/](../docs/agents/) — issue tracker workflow, triage labels, domain doc conventions
  (`CONTEXT.md`, `docs/adr/`).

## Layout

- `cmd/` — Cobra command definitions, one file per command (`init.go`, `up.go`, `qa.go`, `deploy.go`, ...).
- `internal/` — the implementation behind those commands (Docker Compose generation, config
  parsing, the QA and deploy pipelines, templating).
- `templates/` — Docker/Compose/config templates rendered into a user's environment.
- `e2e/` — build-tag-gated (`-tags e2e`) end-to-end tests that provision real environments
  against every supported OroCommerce version; slow, run nightly/on dispatch, not on every push.

## Conventions

- Commit messages: Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`,
  `refactor:`) — these drive the changelog grouping in `.goreleaser.yaml`.
- Lint: `golangci-lint` via `make lint`, config in `.golangci.yml`.
- Unit tests: `make test` (`go test -v ./...`).
- E2E tests: `make e2e` — only run these locally when a change touches `init`, `up`, Docker
  generation, or the OroCommerce install/QA/deploy path; they take a long time.
- Config precedence and merge semantics for QA tool configs (PHPStan, Rector, PHP-CS-Fixer,
  Twig-CS-Fixer, ESLint, Stylelint) are non-trivial — read [docs/qa.md](../docs/qa.md) before
  changing anything under `qa-init`/`qa`.

## Issue tracking

Issues live in GitHub Issues on `algoritma-dev/orobox`, driven through the `gh` CLI — see
[docs/agents/issue-tracker.md](../docs/agents/issue-tracker.md) and
[docs/agents/triage-labels.md](../docs/agents/triage-labels.md) for the labeling scheme.
