# Orobox - CLI Tool for OroCommerce Development

[![CI](https://github.com/algoritma-dev/orobox/actions/workflows/ci.yml/badge.svg)](https://github.com/algoritma-dev/orobox/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/algoritma-dev/orobox?include_prereleases&sort=semver)](https://github.com/algoritma-dev/orobox/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/algoritma-dev/orobox)](go.mod)
[![License: GPL v3](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)
[![Downloads](https://img.shields.io/github/downloads/algoritma-dev/orobox/total)](https://github.com/algoritma-dev/orobox/releases)

Orobox is a command-line tool written in Go that gives you a complete, isolated and reproducible
**OroCommerce** development environment on top of Docker, in one command. It handles PHP, PostgreSQL,
Node.js, Redis, RabbitMQ, Elasticsearch/OpenSearch, Mailpit, nginx, the websocket server, Xdebug and
the whole QA toolchain for you, so you can focus on writing code instead of wiring an
OroPlatform stack.

It supports three modes: a single **bundle** grafted onto a prebuilt OroCommerce application, a whole
OroCommerce **project**, and a production-tuned **demo** instance.

## ⚠️ Important Disclaimer
**WARNING: This tool is designed EXCLUSIVELY for local development. It MUST NOT be used in production environments.** Orobox configures the environment to facilitate debugging and development, which may not comply with security requirements and best practices necessary for a production environment.

## Why Orobox

- **No hand-wired Docker Compose.** A bundle is mounted onto a prebuilt OroCommerce application,
  its resolved `vendor/` is synced back to the host for your IDE, and the test database runs on
  `tmpfs`.
- **QA toolchain included.** PHPStan (with baseline support), Rector, PHP-CS-Fixer, Twig-CS-Fixer,
  ESLint and Stylelint are preconfigured and layered on top of OroCommerce's own standards — see
  [QA tools](docs/qa.md).
- **CI and deploy pipelines generated for you.** `ci-init` writes the GitLab CI pipeline, `deploy-init`
  wires PHP Deployer, and `orobox deploy` runs the whole build/check/release flow — see
  [Deployment](docs/deployment.md).
- **Debugging wired up everywhere.** Xdebug is preinstalled and hot-patchable for the web, CLI,
  consumer and cron processes, and the websocket server is proxied through nginx on the same
  origin as your app.

## Quick start

```bash
orobox create project my-shop
cd my-shop
orobox init
orobox up
```

`init` provisions `.orobox.yaml`, SSL certificates and the Docker files, then runs the OroCommerce
install (restoring a pre-seeded database dump when one is available). `up` starts the stack.

## What you get

| Component | Role |
| --- | --- |
| Nginx | Web server, HTTPS termination, websocket proxying |
| PHP-FPM / PHP-CLI | Application runtime, with Xdebug preinstalled |
| PostgreSQL | Main database (plus a `tmpfs` instance for tests) |
| Redis | Cache and sessions (optional) |
| RabbitMQ | Message broker (optional) |
| Elasticsearch/OpenSearch | Search backend (optional) |
| Mailpit | Captures outgoing email in development |
| Adminer | PostgreSQL admin UI (optional) |
| QA toolchain | PHPStan, Rector, PHP-CS-Fixer, Twig-CS-Fixer, ESLint, Stylelint |

See [Internal structure](docs/development.md) for the full breakdown.

## Installation

### Prerequisites
Before installing Orobox, make sure you have installed on your system:
- **Docker** and **Docker Compose**
- [**mkcert**](https://github.com/FiloSottile/mkcert) (Optional, but required if you want to use **SSL** locally)

### Linux / macOS

Run the following command in your terminal. It will automatically detect your operating system and architecture:

```bash
curl -sSfL "https://github.com/algoritma-dev/orobox/releases/latest/download/orobox_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/')" -o ~/.local/bin/orobox && chmod +x ~/.local/bin/orobox && hash -r
```

*Note: Make sure `~/.local/bin` is in your `PATH`.*

### Windows (PowerShell)

Run the following command in PowerShell:

```powershell
mkdir -Force "$HOME\.local\bin"; iwr "https://github.com/algoritma-dev/orobox/releases/latest/download/orobox_Windows_$($env:PROCESSOR_ARCHITECTURE.ToLower().Replace('amd64','x86_64')).exe" -OutFile "$HOME\.local\bin\orobox.exe"
```

*Note: Make sure to add `%USERPROFILE%\.local\bin` to your User `Path` environment variable.*

### Other installation methods

**Go (any platform):**
```bash
go install github.com/algoritma-dev/orobox@latest
```

**Homebrew (macOS/Linux):**
```bash
brew install algoritma-dev/tap/orobox
```

**Scoop (Windows):**
```powershell
scoop bucket add algoritma-dev https://github.com/algoritma-dev/scoop-bucket
scoop install orobox
```

**deb/rpm/apk:** package files are attached to every [release](https://github.com/algoritma-dev/orobox/releases).

*Homebrew and Scoop require the `algoritma-dev/homebrew-tap` and `algoritma-dev/scoop-bucket`
repositories to be set up — see `.goreleaser.yaml`.*

### Updating

```bash
orobox self-update
```

## Commands at a glance

| Command | What it does | Docs |
| --- | --- | --- |
| `create` | Scaffold a new project or bundle skeleton | [commands.md](docs/commands.md#1-scaffolding-create) |
| `init` | Provision `.orobox.yaml`, SSL, Docker files, and run the Oro install | [commands.md](docs/commands.md#2-initialization-init) |
| `up` / `down` | Start / stop the Docker services | [commands.md](docs/commands.md#3-start-environment-up) |
| `shell` | Open an interactive shell in a container | [commands.md](docs/commands.md#5-shell-access-shell) |
| `logs` | Tail nginx, PHP-FPM, app, consumer, cron or websocket logs | [commands.md](docs/commands.md#6-view-logs-logs) |
| `console` | Run a Symfony console command | [commands.md](docs/commands.md#7-symfony-console-console) |
| `test` | Run the PHPUnit suites | [commands.md](docs/commands.md#8-run-tests-test) |
| `qa-init` / `qa` | Install and run the QA toolchain | [qa.md](docs/qa.md) |
| `clean` | Remove all containers and volumes | [commands.md](docs/commands.md#11-total-cleanup-clean) |
| `run` | Run a custom command from `.orobox.yaml` | [commands.md](docs/commands.md#12-run-custom-commands-run) |
| `deploy-init` / `ci-init` / `deploy` | Configure and run the build/check/release pipeline | [deployment.md](docs/deployment.md) |
| `xdebug` | Hot-patch Xdebug in running containers | [debugging.md](docs/debugging.md) |

## Documentation

- [docs/configuration.md](docs/configuration.md) — `.orobox.yaml`, installation types, environment files, global flags.
- [docs/commands.md](docs/commands.md) — the commands not covered by QA/deployment below.
- [docs/qa.md](docs/qa.md) — the QA toolchain, the PHPStan baseline, the shared vendor tree, CI reports.
- [docs/deployment.md](docs/deployment.md) — `deploy-init`, `ci-init`, `deploy`, caching in CI.
- [docs/debugging.md](docs/debugging.md) — Xdebug for web/CLI/consumer/cron, PHPStorm setup.
- [docs/domains-and-ssl.md](docs/domains-and-ssl.md) — hosts file, mkcert, the websocket proxy.
- [docs/development.md](docs/development.md) — internal structure and contributing with the `Makefile`.
- Full index: [docs/README.md](docs/README.md)

## Comparison with alternatives

See [docs/comparison.md](docs/comparison.md) for how Orobox relates to the official Oro Docker
documentation, community Compose stacks, and generic tools like DDEV, Lando or Warden — including
when *not* to reach for Orobox.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

GPL-3.0 — see [LICENSE](LICENSE). See [docs/promotion/license-analysis.md](docs/promotion/license-analysis.md)
for what that means if you're evaluating Orobox for a commercial project.

**Does using Orobox affect the license of my bundle?** No. Orobox is a development tool you invoke
from the outside — via the command line, against your project's files and containers — never linked
into, imported by, or distributed with your code. The GPL's own FAQ treats this kind of
separate-program invocation (command-line arguments, pipes, subprocess exec) as "mere aggregation,"
not a combined work (see the [GPL FAQ](https://www.gnu.org/licenses/gpl-faq.html#MereAggregation)).
GPL-3.0 governs Orobox's own source and any modified copies of Orobox you distribute; it has no
bearing on the license of the OroCommerce bundle or project you build, test, or deploy with it —
proprietary, MIT, or anything else.

---

Developed to simplify the work of OroCommerce developers.

---

### Inspired by
- [MageBox](https://magebox.dev/)
