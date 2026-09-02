# Orobox documentation

Full reference docs, split out of the [README](../README.md) so it can stay a landing page.

- [Configuration](configuration.md) — `.orobox.yaml`, installation types, environment files, global flags.
- [Commands](commands.md) — `create`, `init`, `up`, `down`, `shell`, `logs`, `console`, `test`, `clean`, `run`.
- [QA tools](qa.md) — `qa-init`, `qa`, the PHPStan baseline, the shared vendor tree, project configuration files, running checks in CI, reports.
- [Deployment](deployment.md) — `deploy-init`, `ci-init`, `deploy`, keeping development files out of a release, pipeline caching.
- [Debugging with Xdebug](debugging.md) — enabling/disabling Xdebug, CLI/consumer/cron, PHPStorm setup.
- [Domains, SSL and WebSocket](domains-and-ssl.md)
- [Internal structure and development](development.md) — the services Orobox wires up, and the contributor `Makefile` targets.
- [Comparison with alternatives](comparison.md) — how Orobox relates to the official Oro Docker docs, community stacks, and generic tools (DDEV, Lando, Warden).
