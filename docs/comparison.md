[← Back to README](../README.md) · [Documentation index](README.md)

# Comparison with alternatives

This page compares Orobox to the other paths a developer setting up an OroCommerce environment
is likely to consider: the official Oro documentation, community Docker Compose stacks found on
GitHub, a hand-rolled Docker Compose file, and generic PHP local-dev tools (DDEV, Lando, Warden).

**How to read this page.** Every factual claim about a third-party project below is either backed
by a link to a page or repository actually fetched or searched while writing this comparison, or
is explicitly marked **not verified**. Where "not verified" appears, it means the search/fetch
performed did not surface a documented answer — not that the feature is absent. Treat those cells
as open questions to check yourself before deciding, not as "no". Nothing here is meant to
disparage the alternatives; several of them solve a broader problem than Orobox does (a generic
PHP/CMS stack for many frameworks, not OroCommerce specifically), and picking a narrower or wider
tool is a legitimate tradeoff either way.

## Comparison table

| Dimension | Orobox | Official Oro dev-environment docs | Community Compose stacks (GitHub) | Manual Docker Compose (no tool) | DDEV | Lando | Warden |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Time to first boot | `orobox create` → `init` → `up`; restores a pre-seeded DB dump instead of a full `oro:install` when the image's dump matches the target version [(docs/commands.md)](commands.md#pre-installed-database) | Manual, multi-step: clone, install services, run Composer/`oro:install` yourself; the "Docker and Symfony Server" guide runs only backing services in Docker and PHP/Node on the host [oroinc docs](https://doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/) | Varies per repository; none seen ship a pre-seeded database, so a full `oro:install` is the default path (not verified for every repo — see [Community stacks](#2-community-docker-compose-stacks-github)) | Slowest: you write the Compose file, wire the network, and run the full Oro installer yourself, from scratch | Fast for its supported presets; OroCommerce is not one of them, so first boot on Oro means writing a custom config from the generic PHP type (not verified how long that takes) | Fast for its own recipes; no official or actively maintained OroCommerce recipe found (see below) | Fast for its supported frameworks; OroCommerce is not one of them |
| Multi-version OroCommerce support (5.1–7.0) | Yes — `5.1`, `6.0`, `6.1`, `7.0` are the versions Orobox builds images and dumps for (`internal/config/config.go: SupportedOroVersions`) | The documentation site itself is versioned per Oro release (5.1, 6.0, 6.1, 7.0, 7.1-dev selectors seen on [doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/)), but that is per-version docs, not one tool that provisions any of them on demand | Not verified in general; the repositories found each target a fixed, often old, version (e.g. `vtsykun/docker-orocommerce` lists 1.6/3.1/4.1/4.2 — [repo](https://github.com/vtsykun/docker-orocommerce)) | N/A — whatever you build supports whatever version you wire it for | Not applicable — no OroCommerce preset exists to version | Not applicable — no maintained OroCommerce recipe exists to version | Not applicable — no OroCommerce environment type exists to version |
| Bundle / project / demo modes | Yes — `bundle` (grafted onto a prebuilt app), `project` (full app checkout), `demo` (prod-tuned) are first-class `type:` values [(configuration.md)](configuration.md#installation-types-type) | No equivalent concept; the guides set up "an application" checkout (closest to Orobox's `project`) and a separate demo-only Docker Compose repo ([oroinc/orocommerce-application-demo-docker](https://github.com/oroinc/orocommerce-application-demo-docker)) | Not verified; the repositories found each ship one docker-compose.yml for "an OroCommerce app", with no bundle-grafting concept observed | N/A — you decide the shape yourself | No concept of bundle/project/demo modes | No concept of bundle/project/demo modes | No concept of bundle/project/demo modes |
| Included QA toolchain | Yes — PHPStan (with baseline), Rector, PHP-CS-Fixer, Twig-CS-Fixer, ESLint, Stylelint, preconfigured and layered on Oro's own standards, installed by `qa-init` and run by `qa` [(qa.md)](qa.md) | Documentation *recommends* configuring PHPStorm with Symfony/Oro plugins, PHP_CodeSniffer, PHP Mess Detector, PHP-CS-Fixer and PHPUnit — IDE setup guidance, not an automated, versioned toolchain shipped with the environment ([doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/)) | Not verified — no evidence found of a bundled/preconfigured QA toolchain in the repositories checked | None — you install and wire every tool yourself | Not Oro-specific; DDEV provides the container platform, not an Oro QA toolchain | Not Oro-specific | Not Oro-specific |
| CI pipeline generation | Yes — `ci-init` writes a ready-to-run GitLab CI pipeline (`lint`, `test`, `deploy` jobs) from `.orobox.yaml`, `project` type only [(deployment.md)](deployment.md#14-ci-initialization-ci-init) | Jenkins CI is documented as a configuration option ([doc.oroinc.com/backend/setup/jenkins](https://doc.oroinc.com/backend/setup/jenkins/)), but this is a setup guide, not a generator invoked from a project's own config | Not verified | None | Not Oro-specific; no generator for an Oro pipeline | Not Oro-specific | Not Oro-specific |
| Deploy tooling | Yes — `deploy-init` wires PHP Deployer with an Oro-specific recipe, `deploy` runs build/check/release through Dagger, `project` type only [(deployment.md)](deployment.md) | General Symfony deployment guidance is referenced (lock `composer.lock`, warm prod cache, configure cron/websocket); no dedicated Oro deploy-tool integration was found in the pages checked (not verified beyond that) | Not verified | None | Not Oro-specific | Not Oro-specific | Not Oro-specific |
| Xdebug preconfiguration | Yes — preinstalled but disabled by default, hot-patchable per running container with `orobox xdebug enable/disable`, independently for web, CLI, consumer and cron [(debugging.md)](debugging.md) | Documented as a manual PHPStorm/Xdebug setup step ([doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/)); the "Docker and Symfony Server" guide runs PHP on the host, so Xdebug is whatever the host PHP install provides, not container-managed ([doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/)) | Not verified | None built in | Yes, generically — `ddev xdebug on/off/toggle/status` ([DDEV docs via search](https://docs.ddev.com/en/stable/users/debugging-profiling/step-debugging/)); not Oro-aware | Yes, generically — recipes expose an `xdebug: true` option ([Lando docs](https://docs.lando.dev/core/v3/recipes.html)); no Oro recipe to attach it to | Yes, generically — Xdebug configuration is documented ([docs.warden.dev](https://docs.warden.dev/)); not Oro-aware |
| Websocket support | Yes — the Oro websocket server is proxied by nginx on the same origin as the app (`wss://` on SSL domains), with the addressing wired automatically for web/ws/consumer/cron [(domains-and-ssl.md)](domains-and-ssl.md#websocket) | Not verified — not covered in the pages checked | Not verified | None — you configure the proxy yourself if you need it | Not Oro-specific — no built-in Oro websocket wiring | Not Oro-specific | Not Oro-specific; Warden does provide a shared Traefik proxy, but no Oro websocket wiring was found |
| tmpfs test DB | Yes — the test database container can run on `tmpfs`, with a configurable size, for fast disposable test runs [(configuration.md)](configuration.md#configuration-fields) | Not verified — not covered in the pages checked | Not verified | None — up to you to configure | Not verified for Oro use (no Oro preset) | Not verified for Oro use (no Oro recipe) | Not verified for Oro use (no Oro environment type) |
| Vendor sync for IDE | Yes — for `type: bundle`, the `vendor/` resolved inside the container is synced back to the host so the IDE can index it [(configuration.md)](configuration.md#installation-types-type) | N/A — the "Docker and Symfony Server" guide runs Composer on the host directly, so there is nothing to sync back in that setup ([doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/)); the containerized demo repo's behavior here is not verified | Not verified | None — if vendor is resolved only inside a container, syncing it back is your own script | Not Oro-specific; general DDEV projects typically run Composer inside the web container with the project directory mounted, so vendor is visible on the host by default for that generic setup (not verified for a custom Oro type specifically) | Not Oro-specific / not verified | Not Oro-specific / not verified |
| Private Composer repo handling (tokens + SSH agent forwarding) | Yes — `composer.auth` mirrors Composer's `COMPOSER_AUTH` schema and is injected only into containers that run Composer; SSH-transport repository URLs trigger automatic host SSH-agent forwarding, with an explicit `ssh_agent` override [(configuration.md)](configuration.md#configuration-fields) | Composer's own private-package authentication mechanisms apply (`auth.json`, `COMPOSER_AUTH`) as they would to any Composer project; no Oro-specific tooling for this was found in the pages checked (not verified beyond that) | Not verified | None — you wire Composer credentials and SSH agent forwarding into the Compose file yourself | Not Oro-specific; DDEV has general Composer/SSH-key support, but no Oro-specific wiring (not verified in depth) | Not Oro-specific / not verified | Not Oro-specific / not verified |

## 1. Official Oro dev-environment documentation

The current [`doc.oroinc.com/backend/setup/dev-environment/`](https://doc.oroinc.com/backend/setup/dev-environment/)
section documents a **hybrid** setup as its primary path — "Docker and Symfony Server" — where
Docker runs only backing services (PostgreSQL, Elasticsearch, RabbitMQ, Redis, MailCatcher) while
PHP, Composer and Node.js run directly on the host and are invoked through the Symfony CLI
([doc.oroinc.com](https://doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/)). It
ships OS-specific quick guides for macOS, Ubuntu and WSL2, and the docs explicitly note the
Symfony local web server "is not intended for production use."

Oro also documents a fully containerized alternative for demo/evaluation purposes:
[`doc.oroinc.com/backend/setup/demo-environment/docker/`](https://doc.oroinc.com/backend/setup/demo-environment/docker/),
backed by the [`oroinc/orocommerce-application-demo-docker`](https://github.com/oroinc/orocommerce-application-demo-docker)
repository, which packages web server, PHP-FPM, consumer, websocket and cron roles into images
published on Docker Hub. That page is explicit: **"This deployment is NOT intended for a
production environment."** It supports switching between OroCommerce versions (5.1 through the
current release and a 7.1 dev line, per the version selector on the docs site) but is a demo
convenience, not a development toolchain — it does not mention Xdebug, a QA toolchain, CI
generation, or deploy automation in the material checked.

Both official paths document *how a human sets up the pieces*; neither is a CLI that scaffolds a
bundle/project skeleton, generates CI/deploy pipelines, or hot-patches Xdebug per process the way
Orobox does. That is a reasonable difference in scope, not a shortcoming — the official docs also
cover ground Orobox does not, such as OS-native (non-Docker) host setup.

## 2. Community Docker Compose stacks (GitHub)

A GitHub Topics/search pass surfaced several independent, unofficial Docker stacks for Oro-family
applications ([search results](https://github.com/topics/orocommerce)):

- [`kiboko-labs/kloud`](https://github.com/kiboko-labs/kloud) — referenced by Oro's own docs as
  "Docker images and stacks for OroPlatform based applications by the Kiboko team." Its README
  describes building a local Docker stack for OroCommerce/OroCRM/OroPlatform/Marello development,
  explicitly not for production. The repository shows 155 commits and multiple open issues/PRs;
  its current maintenance cadence was **not verified** (no commit date was visible in what was
  fetched).
- [`vtsykun/docker-orocommerce`](https://github.com/vtsykun/docker-orocommerce) — a compact
  Alpine-based image bundling web server, cron, websocket and job processing in one container, for
  OroCommerce **1.6, 3.1, 4.1 and 4.2** — versions well before Orobox's supported 5.1–7.0 range.
  Current maintenance status **not verified**.
- [`djocker/orocommerce`](https://github.com/djocker/orocommerce) ("Dockerized OroCommerce
  Application (Unofficial)") and [`mkovel/orodeploytemplate`](https://github.com/mkovel/orodeploytemplate)
  turned up in the same search; their content, target versions and maintenance status were **not
  independently verified** beyond their titles and descriptions in search results.
- A Lando-specific community image, [`improper/lando-image-orocommerce`](https://github.com/improper/lando-image-orocommerce),
  extends Lando's PHP image with OroCommerce's PHP extension dependencies. Its own README says the
  author "hope[s] to create a Lando Recipe to automate this setup" — it never became one — and the
  repository **was archived (read-only) on 2020-08-01**, per GitHub's archive notice.

General pattern observed across this category: these are individually maintained images/compose
files targeting a specific, often dated, Oro version, built to solve one person's or team's setup
problem rather than to track Oro's supported version matrix or provide QA/CI/deploy automation. No
code-search access was used to audit their Dockerfiles or compose files line by line — the claims
above come from each repository's own README/description as fetched, not from reading their
implementation.

## 3. A fully manual Docker Compose setup (no tool)

Writing your own `docker-compose.yml` for OroCommerce — one PHP-FPM service, one Postgres, nginx,
Node for asset building, and whichever of Redis/RabbitMQ/Elasticsearch/Mailpit you need — gives
the most control and the fewest assumptions to unwind, at the cost of building and maintaining
every piece yourself: the OroCommerce Docker image and PHP extension set, Composer/SSH-agent
wiring for private packages, Xdebug toggling, a websocket reverse-proxy configuration, a disposable
tmpfs test database, a QA toolchain, and any CI/deploy pipeline. None of that is hard individually,
but all of it has to be built once, then kept in sync by hand as OroCommerce's own requirements
(PHP version, extensions, Node version) change across releases — work a tool like Orobox, the
official Oro images, or a generic platform like DDEV/Lando/Warden has already done once for many
projects to share. The tradeoff is the classic build-vs-adopt one: total control and zero
tool lock-in, versus paying setup and maintenance cost per project instead of once per tool.

## 4. Generic local-dev tools: DDEV, Lando, Warden

These three are real, actively maintained, general-purpose local Docker environment tools for
PHP/CMS stacks. None of them ships official, out-of-the-box OroCommerce support as of this
writing:

- **DDEV** — its homepage advertises "presets for Laravel, WordPress, Drupal, TYPO3, Backdrop,
  Magento, Craft CMS and more" ([ddev.com](https://ddev.com/)); OroCommerce is not among the named
  presets, and none was found by searching DDEV's docs. DDEV does support a `generic`/`php`
  project type meant for stacks without a dedicated preset, plus general Composer and SSH-key
  support, and ships built-in Xdebug toggling (`ddev xdebug on/off/toggle/status`)
  ([DDEV docs](https://docs.ddev.com/en/stable/users/debugging-profiling/step-debugging/) — found
  via search; the docs page fetched directly returned an error). Running OroCommerce on DDEV would
  mean building a custom project-type configuration yourself, similar in spirit to the manual
  Compose route above but on top of DDEV's platform primitives.
- **Lando** — its recipes documentation points to a separate plugins listing rather than a fixed
  in-page list ([docs.lando.dev](https://docs.lando.dev/core/v3/recipes.html)); no official Symfony
  or OroCommerce recipe was found there. The one OroCommerce-oriented Lando artifact located,
  [`improper/lando-image-orocommerce`](https://github.com/improper/lando-image-orocommerce), is an
  unofficial community image whose author never completed the intended recipe and which has been
  archived since 2020, so it is not a currently maintained path.
- **Warden** — its own docs list explicit support for "Magento 1, Magento 2, Laravel, Symfony 4,
  Shopware 6" on macOS and Linux ([docs.warden.dev](https://docs.warden.dev/)), plus a `Local` and
  a fully custom environment type; OroCommerce is not among the named environments. Warden provides
  shared infrastructure (Traefik SSL/routing, DNS, Xdebug configuration support) that a custom
  environment definition could build on, but no ready-made Oro environment was found.

For a team already standardized on one of these tools for other PHP projects, the realistic choice
is between adopting Orobox specifically for Oro work, or investing in a custom Oro configuration on
top of the tool they already use — both are legitimate paths, and the effort of the latter was not
measured here.

## When *not* to use Orobox

- **Non-Oro projects.** Orobox is purpose-built around OroCommerce/OroPlatform's install flow,
  bundle structure, QA standards and deploy recipe. It has nothing to offer a Laravel, WordPress,
  Drupal, Magento or plain Symfony project — a generic tool like DDEV, Lando or Warden (or a
  framework-specific one) fits that work better.
- **Production environments.** Orobox says so directly: *"This tool is designed EXCLUSIVELY for
  local development. It MUST NOT be used in production environments."* ([README.md](../README.md))
  The `demo` install type is production-*tuned* for realistic performance testing, but it is still
  generated and run by the same dev-only tool — it is not a production deployment mechanism.
  `orobox deploy` ships application releases to a remote host via Deployer, but the local Orobox
  stack itself remains a development environment, never the production runtime.
- **Teams with an already-consolidated internal Docker stack.** If an organization already
  maintains its own Compose files, base images, CI templates and deploy scripts across several Oro
  projects — tuned to its own infrastructure, registries and conventions — swapping that for
  Orobox means migrating working tooling for a rewrite of already-solved problems. Orobox is aimed
  at projects and teams that do not already have that investment, or that are starting fresh; it is
  not designed to replace a mature, working internal stack just because it exists.

## Sources consulted

- [doc.oroinc.com/backend/setup/dev-environment/](https://doc.oroinc.com/backend/setup/dev-environment/)
- [doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/](https://doc.oroinc.com/backend/setup/dev-environment/docker-and-symfony/)
- [doc.oroinc.com/backend/setup/demo-environment/docker/](https://doc.oroinc.com/backend/setup/demo-environment/docker/)
- [doc.oroinc.com/backend/setup/jenkins/](https://doc.oroinc.com/backend/setup/jenkins/)
- [github.com/oroinc/orocommerce-application-demo-docker](https://github.com/oroinc/orocommerce-application-demo-docker)
- [github.com/topics/orocommerce](https://github.com/topics/orocommerce)
- [github.com/kiboko-labs/kloud](https://github.com/kiboko-labs/kloud)
- [github.com/vtsykun/docker-orocommerce](https://github.com/vtsykun/docker-orocommerce)
- [github.com/improper/lando-image-orocommerce](https://github.com/improper/lando-image-orocommerce)
- [ddev.com](https://ddev.com/)
- [docs.ddev.com — step debugging (via search, direct fetch failed)](https://docs.ddev.com/en/stable/users/debugging-profiling/step-debugging/)
- [docs.lando.dev/core/v3/recipes.html](https://docs.lando.dev/core/v3/recipes.html)
- [docs.warden.dev](https://docs.warden.dev/)
- [docs.warden.dev/environments/types.html](https://docs.warden.dev/environments/types.html)

No GitHub code-search access was available for this comparison — findings about community
repositories are based on their README/description text as fetched or as returned by search
results, not on reading their Dockerfiles or Compose definitions.
