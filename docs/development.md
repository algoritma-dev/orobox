[← Back to README](../README.md) · [Documentation index](README.md)

# Internal structure and development

## Internal Structure
The Orobox environment typically includes:
- **Nginx**: Web server configured for OroCommerce.
- **PHP-FPM / PHP-CLI**: Runtime for the application and Symfony commands.
- **PostgreSQL**: Main database.
- **Redis**: For cache and sessions (optional).
- **RabbitMQ**: Message broker (optional).
- **Elasticsearch/OpenSearch**: For search features (optional).
- **Mailpit**: To capture emails sent during development (optional).

## Development

If you want to contribute to Orobox, you can use the provided `Makefile` to simplify common tasks.

### Prerequisites
- **Go** (version 1.26 or later)
- **golangci-lint** (recommended version 1.64.0 or later)

### Available Commands
- **Linting**:
  ```bash
  make lint
  ```
- **Running Tests**:
  ```bash
  make test
  ```
- **Local Build**:
  ```bash
  make build
  ```
- **Update Version**:
  ```bash
  make set-version v=X.Y.Z
  ```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full contributor workflow (lint, unit tests, e2e suite, commit conventions).
