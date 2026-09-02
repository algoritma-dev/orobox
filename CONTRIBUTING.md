# Contributing to Orobox

Thanks for taking the time to contribute.

## Prerequisites

- **Go** 1.26 or later
- **Docker** and **Docker Compose**
- [**mkcert**](https://github.com/FiloSottile/mkcert) if you need to exercise SSL locally
- **golangci-lint** (recommended 1.64.0+) for `make lint`

## Development workflow

The `Makefile` wraps the common tasks:

```bash
make lint    # golangci-lint run ./... (config: .golangci.yml)
make test    # go test -v ./...
make build   # go build -o orobox main.go
make e2e     # build, then the full e2e suite (see below)
```

### Tests

- **Unit tests** (`make test`) run on every push and PR via `.github/workflows/ci.yml`.
- **End-to-end tests** (`e2e/`, build-tagged `e2e`) spin up real Orobox environments against
  every supported OroCommerce version (5.1 through 7.0) and only run nightly / on manual dispatch,
  because they take hours. See [`e2e/README.md`](e2e/README.md) before touching them, and run
  `make e2e` locally if your change affects `init`, `up`, or anything Docker/Oro-install related.

### Lint

`golangci-lint` config lives in `.golangci.yml`. `make lint` installs the pinned version
automatically if it's missing.

## Commit messages

This repository follows [Conventional Commits](https://www.conventionalcommits.org/):
`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`, `refactor:`. These prefixes drive the
release changelog grouping in `.goreleaser.yaml` — `docs:`, `test:` and `chore:` commits are
excluded from it, and `feat:`/`fix:` are grouped into their own sections.

## Branching and review

- `main` is the trunk; open PRs against it.
- Every PR runs `lint` and `test` in CI; keep both green.
- A maintainer reviews and merges — see [CODEOWNERS](.github/CODEOWNERS) for who is
  auto-requested.

## Proposing a new feature

Open a [discussion or issue](https://github.com/algoritma-dev/orobox/issues/new/choose) before
sending a large PR, so the design can be discussed before the implementation is.

## Release policy

Orobox uses [SemVer](https://semver.org/). A release candidate (`X.Y.Z-rcN`) is cut for
pre-release validation; GoReleaser marks it as a GitHub pre-release
(`release.prerelease: auto` in `.goreleaser.yaml`), so `releases/latest` and
`orobox self-update` keep pointing at the newest **stable** tag until a plain `X.Y.Z` is
tagged. A stable release is cut once the checklist in the tracking issue for that version is
complete (see the "Release 1.0.0" issue for the current one).
