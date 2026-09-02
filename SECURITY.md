# Security Policy

## Supported versions

Orobox does not yet have a stable 1.0 release; only the latest tagged release (stable or
release candidate) is supported. Older tags do not receive security fixes.

## Reporting a vulnerability

Please use GitHub's [private vulnerability reporting](https://github.com/algoritma-dev/orobox/security/advisories/new)
for this repository (Settings → Security → enable "Private vulnerability reporting" — see the
manual checklist this depends on). If that is not available, email
**raffaele.carelle@algoritma.it** with details and, if possible, a reproduction.

Please do not open a public issue for a suspected vulnerability.

We aim to acknowledge reports within **5 business days** and to provide a remediation timeline
within **14 days** of confirming the issue.

## What is *not* a vulnerability report

Orobox is a **local development tool only** — see the disclaimer at the top of the
[README](README.md). Its defaults are chosen for developer convenience, not production hardening,
and reporting them as vulnerabilities just adds noise for everyone:

- Xdebug enabled/available in images.
- Adminer (a PostgreSQL admin UI) exposed on a local port.
- Fixed/well-known development credentials (database, admin user, etc.).
- OPcache disabled or `validate_timestamps` left on in `bundle`/`project` mode.

These are all intentional for a `bundle`/`project` (development) environment. Running any
Orobox-generated stack in production, or exposing it beyond `localhost`, is explicitly
unsupported — see the `demo` install type in [docs/configuration.md](docs/configuration.md)
for the closest thing Orobox offers to a production-tuned profile, and note that even that mode
is documented as being for demo/staging, not production traffic.

Genuine issues we do want reported: anything that lets a container escape its intended
boundary, a credential or token (e.g. from `composer.auth`) leaking into a log, image layer, or
committed file it shouldn't reach, or a supply-chain issue in how Orobox builds/distributes its
own binaries.
