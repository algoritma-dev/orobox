# Awesome-list submission prep

Internal prep material only — no pull request or issue has been opened against any of these
repositories. This is a checklist of candidate "awesome list" repos, the real (verified) admission
requirements for each, and a ready-to-paste entry, so that a submission can be filed quickly once
Orobox is in shape for it.

Researched 2026-09-02, against Orobox at commit `d4010de` (branch `create-scaffold`), repo
`algoritma-dev/orobox`: 4 stars, created 2026-03-19, `go.mod` module `github.com/algoritma-dev/orobox`,
license GPL-3.0, latest tag `1.0.0-rc30` (no stable `1.0.0` release yet, and no tag uses the `v`
prefix Go modules require for SemVer resolution).

## Summary

| List | Verdict | Status |
| --- | --- | --- |
| [avelino/awesome-go](#1-avelinoawesome-go) | Viable, not yet | blocked on: no `vX.Y.Z` tag |
| [veggiemonk/awesome-docker](#2-veggiemonkawesome-docker) | Viable now | requirements met |
| [sitepoint-editors/awesome-symfony](#3a-sitepoint-editorsawesome-symfony) | Stretch, low odds | not recommended: list appears effectively unmaintained for new entries |
| [ziadoz/awesome-php](#3b-ziadozawesome-php) | Stretch, poor fit | not recommended: scope is Composer/PHP packages, Orobox is a Go binary |
| Oro/OroCommerce-specific awesome list | Does not exist | no candidate found |

---

## 1. avelino/awesome-go

- **URL:** https://github.com/avelino/awesome-go
- **Target section:** `Software Packages` → `DevOps Tools` (this is where comparable Docker-based
  dev-environment CLIs already live, e.g. `kool` — "Command line tool for managing Docker
  environments as an easy way.")
- **Requirements source:** https://github.com/avelino/awesome-go/blob/main/CONTRIBUTING.md
  (fetched live 2026-09-02; the list is active — last push same day)

### Ready-to-paste entry

Alphabetically between `Mora` and `ostent`:

```md
- [Orobox](https://github.com/algoritma-dev/orobox) - Provisions a full, disposable OroCommerce development stack in Docker (PHP, PostgreSQL, Redis, RabbitMQ, Elasticsearch/OpenSearch) with one command.
```

PR-body links the guidelines ask for:

```
Forge link: https://github.com/algoritma-dev/orobox
pkg.go.dev: https://pkg.go.dev/github.com/algoritma-dev/orobox
goreportcard.com: (service sunset 2026-07-01, dead — omit or replace with golangci-lint report link)
Coverage: (none published yet)
```

### Requirements checked against Orobox's current state

| Requirement (verbatim/paraphrased from CONTRIBUTING.md) | Met? | Notes |
| --- | --- | --- |
| ≥5 months of history since first commit | **Yes** | First commit 2026-03-19; ~5.5 months old as of 2026-09-02. |
| Open source license | **Yes** | GPL-3.0, on OSI's approved list. |
| `go.mod` present at repo root | **Yes** | `module github.com/algoritma-dev/orobox`. |
| **At least one tag matching `vX.Y.Z`** (blocking CI check) | **No** | Existing tags are `1.0.0-rc30` etc. — no `v` prefix, and they carry a pre-release suffix. Go's module resolver only recognizes `v`-prefixed tags, so none of these are usable as a real module version; `go install ...@latest` currently resolves to a pseudo-version, not a tagged release. **This is the actual blocker.** |
| Go Report Card grade A-/A/A+ (blocking CI check) | **Not verified — the check itself may be dead** | `goreportcard.com` announced it sunset the live service on 2026-07-01 (servers shut down, repo archived), yet avelino/awesome-go's `CONTRIBUTING.md` fetched today (2026-09-02) still lists it as a blocking automated check and still asks for a `goreportcard.com` link in the PR body. Confirmed dead 2026-09-02 (site abandoned) — removed the dead badge from Orobox's own README. This is a live contradiction in avelino/awesome-go's own process worth re-checking at submission time — it may have been patched to an alternative (e.g. `golangci-lint`) by then. |
| pkg.go.dev reachable | **No, not yet** | `https://pkg.go.dev/github.com/algoritma-dev/orobox` returns 404 today. This is fixable without a stable release — pkg.go.dev indexes any module version once the Go proxy is asked for it — but it hasn't been triggered yet. |
| Test coverage ≥80% (non-data packages), coverage link | **Not verified** | 58 `_test.go` files exist and CI runs `go test -v ./...`, but no coverage flag, Codecov, or Coveralls integration exists, so there's no number or link to cite. |
| README + pkg.go.dev doc comments in English | **Yes** | README is thorough and in English; doc-comment coverage on exported symbols wasn't audited here. |
| Category has ≥3 items | **Yes (trivially)** | `DevOps Tools` already lists 100+ projects. |
| Actively maintained (regular commits / issues answered within ~2 weeks) | **Yes** | 307 commits, latest push 2026-08-31, only 2 open issues. |
| No explicit minimum star count | **N/A** | The guidelines never state a star threshold; "generally useful to the community" is judged manually by reviewers, not gated by stars. 4 stars is a real weakness for that manual judgment call, but it isn't a documented hard requirement. |

**Status: blocked on: no stable/SemVer-tagged release (`vX.Y.Z`) exists yet.** Everything else is
either already satisfied or fixable in minutes (trigger pkg.go.dev indexing, add coverage). Cut a
`v1.0.0`-style tag (or at least `v0.x.y`) before opening the PR, and re-verify the Go Report Card
requirement at that time since the underlying service is defunct.

---

## 2. veggiemonk/awesome-docker

- **URL:** https://github.com/veggiemonk/awesome-docker
- **Target section:** `Developer Workflow` → `Development Environment`
- **Requirements source:**
  https://github.com/veggiemonk/awesome-docker/blob/master/.github/CONTRIBUTING.md and
  https://github.com/veggiemonk/awesome-docker/blob/master/.github/MAINTENANCE.md (fetched live
  2026-09-02)

### Why this one and not another awesome-docker

There are several forks/clones of "awesome-docker" on GitHub; `veggiemonk/awesome-docker` is the
canonical, actively maintained one — not archived, pushed 2026-08-27, with merged PRs as recent as
2026-08-18 (e.g. adding Laradock, adding a private registry entry). It even ships its own Go-based
maintenance CLI, CI workflows for weekly link/health checks, and a documented review cadence
(`.github/MAINTENANCE.md`), which is a stronger maintenance signal than most awesome-lists provide.

### Ready-to-paste entry

Alphabetically between `Laradock` and `uniget` in `Development Environment`:

```md
- [Orobox](https://github.com/algoritma-dev/orobox) - Spins up a full, disposable OroCommerce (Symfony) development environment on Docker Compose — PHP-FPM, PostgreSQL, Redis, RabbitMQ, Elasticsearch/OpenSearch and a QA toolchain — in one command.
```

### Requirements checked against Orobox's current state

| Requirement (from CONTRIBUTING.md) | Met? | Notes |
| --- | --- | --- |
| Passes the "for Docker" test: *"If you removed the Docker integration, would the project still have a reason to exist?"* | **Yes** | Orobox's entire reason to exist is generating and orchestrating a Docker Compose stack (nginx, PHP-FPM, Postgres, Redis, RabbitMQ, Elasticsearch/OpenSearch, Mailpit) for OroCommerce. Remove Docker and there's nothing left — same shape as the already-accepted `dde`, `DIP`, `EnvCLI` and `Lando` entries in the same section. |
| One link per entry, prefer GitHub repo URL over marketing page | **Yes** | `https://github.com/algoritma-dev/orobox`. |
| Alphabetically sorted, case-insensitive | **Yes** | `Orobox` sits between `Laradock` and `uniget`. |
| Description: one sentence, concise, Docker connection obvious | **Yes** | Draft entry above names "Docker Compose" explicitly. |
| Project active in the last 2 years | **Yes** | Created 2026-03-19, pushed 2026-08-31. |
| Not archived/deprecated | **Yes** | Active repo. |
| `make lint` / `./awesome-docker validate` pass (local tooling, can't be verified without cloning their repo) | **Not verified** | Requires cloning `veggiemonk/awesome-docker` and running their CLI; not done as part of this prep. |

**Status: requirements met.** This is the strongest, fastest candidate — no blockers identified.
The only unverified item is their own local lint tool, which can only be checked by actually
opening the PR.

---

## 3a. sitepoint-editors/awesome-symfony

- **URL:** https://github.com/sitepoint-editors/awesome-symfony
- **Target section (if pursued):** `Development` (Symfony bundles/dev tooling)
- **Requirements source:**
  https://github.com/sitepoint-editors/awesome-symfony/blob/master/CONTRIBUTING.md

### Relevance assessment: a stretch, and currently a bad bet

Orobox is a Go CLI wrapping Docker for a Symfony-based e-commerce platform — it's Symfony-adjacent
by virtue of OroCommerce being built on Symfony, but it is not a Symfony bundle, library, or
package, which is what every single entry in this list's `Development` section is (e.g.
`LiipFunctionalTestBundle`, `PsyshBundle`, `TwigReflectionBundle` — all Composer-installed
`*Bundle` classes). The submission format itself, `[BUNDLE NAME](LINK) - DESCRIPTION.`, presumes
a bundle.

Beyond the scope mismatch, the list looks stale in practice despite not being archived:

- The last **merged** PR was "Remove Google+", 2022-12-12 — almost 4 years ago.
- Recent PRs are still opened but are being **closed without merging**: e.g. PR #137, "Add Govard -
  Local development orchestrator with Symfony support" (a near-identical pitch to Orobox's), was
  opened and closed unmerged on 2026-08-23 with zero comments. Two more entries (an import/export
  bundle, a third-party API) were closed unmerged in 2026-08-11 and 2026-01-03 respectively.

That pattern — PRs still arriving, none getting merged, no explanation given — suggests the
maintainer isn't actively curating new entries right now, regardless of what CONTRIBUTING.md says.

### If pursued anyway — ready-to-paste entry

```md
- [Orobox](https://github.com/algoritma-dev/orobox) - CLI that provisions a full, disposable Docker environment for OroCommerce, a Symfony-based e-commerce platform.
```

**Status: not recommended.** Poor category fit (it's a bundle list, Orobox isn't a bundle) compounded
by what looks like a de facto stalled review process. Low expected return for the effort of a PR.

---

## 3b. ziadoz/awesome-php

- **URL:** https://github.com/ziadoz/awesome-php
- **Target section (if pursued):** `Command Line` or possibly `E-commerce`
- **Requirements source:** https://github.com/ziadoz/awesome-php/blob/master/CONTRIBUTING.md
  (fetched live 2026-09-02; this list *is* actively maintained — pushed 2026-07-13, 32.6k stars,
  the genuinely canonical awesome-php)

### Relevance assessment: not a good fit

This list explicitly curates "packages, frameworks, tools, and software the PHP ecosystem has to
offer," with stated preference for software that:

> Targets supported versions of PHP … Can be installed using Composer … Is PSR compliant …

Its `Command Line` section (`*Libraries related to the command line.*`) is, in practice, a list of
PHP libraries for *building* CLI tools (`Aura.Cli`, `CLIFramework`, `GetOpt`, …), not standalone
CLI applications distributed as binaries. Orobox is a compiled Go binary with no Composer package,
no PHP namespace, and nothing PSR-compliant to point at — it isn't PHP software at all, it's a
devops tool that happens to orchestrate a PHP application's runtime. The `E-commerce` section is
likewise PHP packages/platforms (Sylius, Shopware, payment libraries), not tooling.

### If pursued anyway — ready-to-paste entry

```md
* [Orobox](https://github.com/algoritma-dev/orobox) - A Go CLI that provisions a Dockerized local development environment for OroCommerce.
```

(Note the list uses `*` bullets, not `-`, throughout its README — matched here for format
correctness even though the entry itself isn't recommended.)

**Status: not recommended.** Genuine scope mismatch: this is a PHP-package list and Orobox is a
non-PHP binary. Submitting here risks a fast, justified rejection rather than just low odds.

---

## 4. Oro/OroCommerce-specific awesome list

Searched GitHub (`orocommerce awesome`, `oro awesome in:name`, plus general web search) for any
curated "awesome-oro" or "awesome-orocommerce" list. **None exists.** The only hits were unrelated
repos that happen to contain "oro" or "awesome" in their name/description (a course project named
`FirstAwesomeOrojectSWE`, an unrelated Unity game called `awesome-platformer` by a user named
`francisco-oro`). Oro Inc.'s own documentation and the OroCommerce marketplace are not
community-curated "awesome lists" in the sindresorhus sense.

**Status: no candidate — none invented.** If one is created in the future by the Oro community, it
would be the best-fit venue for Orobox by far; worth re-checking periodically (e.g. searching
`orocommerce` or `oro platform` combined with `awesome` on GitHub).

---

## Recommended order of action

1. **veggiemonk/awesome-docker** — ready to submit now, best fit, most active list.
2. **avelino/awesome-go** — cut a proper `vX.Y.Z` tag (e.g. `v1.0.0` or `v0.9.0`) and trigger
   pkg.go.dev indexing first; re-check whether the Go Report Card requirement has been updated
   since the service's 2026-07-01 sunset.
3. **awesome-symfony / awesome-php** — skip, or treat as a low-priority long shot; both are a poor
   scope fit and, in awesome-symfony's case, the review process looks effectively stalled.
4. **Oro-specific list** — nothing to do; revisit if the ecosystem ever produces one.
