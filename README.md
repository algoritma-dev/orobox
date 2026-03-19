# Orobox - CLI Tool for OroCommerce Bundle Development

Orobox is a command-line tool (CLI) developed in Go to quickly set up an isolated and reproducible development environment for OroCommerce bundles. It allows developers to focus on writing bundle code without worrying about the complex configuration of the entire OroCommerce ecosystem.

## ⚠️ Important Disclaimer
**WARNING: This tool is designed EXCLUSIVELY for local development. It must NOT be used in production environments.** Orobox configures the environment to facilitate debugging and development, which may not comply with the security requirements and best practices necessary for a production environment.

## Prerequisites
Before installing Orobox, ensure you have the following installed on your system:
- **Docker** and **Docker Compose**

## Installation

Run the following command in your terminal. It will automatically detect your operating system and architecture:

```bash
curl -sSfL "https://github.com/algoritma-dev/orobox/releases/download/0.0.2-dev/orobox_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/')" -o ~/.local/bin/orobox && chmod +x ~/.local/bin/orobox && hash -r
```

*Note: Make sure `~/.local/bin` is in your `PATH`.*

## Configuration (`.orobox.yaml`)

Orobox uses a configuration file named `.orobox.yaml` in your bundle's root. If the file does not exist, the `init` command will guide you through its interactive creation.

Example `.orobox.yaml` file:
```yaml
type: bundle
class: MyBundle
namespace: MyVendor\Bundle\MyBundle
oro_version: "6.1"
domains:
  - host: mybundle.test
    root: public
    ssl: true
services:
  postgres: "16.1-alpine"
  redis: "7.2-alpine"
  mailpit: true
  php:
    version: "8.4"
    xdebug: true
  node_version: "22"
  npm_version: "10"
  rabbitmq: "3.12-management-alpine"
  elasticsearch: "8.4.1"
```

### Global Flags
These options can be used with any command:
- `--config`: Specify an alternative configuration file (default: `.orobox.yaml`).

## Command Usage

The main command is `oro` (or `orobox`, depending on how you installed it).

### 1. Initialization (`init`)
Prepares the development environment in your bundle's repository.
```bash
orobox init
```
This command:
- Creates the `.orobox.yaml` file if missing (interactive mode).
- Generates SSL certificates if requested.
- Configures the necessary Docker files.

Options:
- `--bundle-path`, `-b`: Bundle path (default ".").
- `--oro-version`, `-v`: OroCommerce version to use (default "6.1").
- `--bundle-namespace`, `-n`: Bundle namespace (e.g., "MyVendor/Bundle/MyBundle").

### 2. Start Environment (`up`)
Starts Docker containers and configures OroCommerce.
```bash
orobox up
```
The command dynamically generates the `docker-compose.yml` file, starts the services, and proceeds with the installation or update of the environment.

### 3. Stop Environment (`down`)
Shuts down the Docker services associated with the project.
```bash
orobox down
```

### 4. Shell Access (`shell`)
Accesses a container interactively (default: php).
```bash
orobox shell
```

### 5. Run Tests (`test`)
Runs PHPUnit tests within the configured environment.
```bash
orobox test
```

### 6. Fresh Start (`clean`)
Removes all associated containers and volumes to start fresh.
```bash
orobox clean
```

## Internal Structure
The Orobox environment typically includes:
- **Nginx**: Web server configured for OroCommerce.
- **PHP-FPM / PHP-CLI**: Runtime for the application and Symfony commands.
- **PostgreSQL**: Primary database.
- **Redis**: For caching and sessions (optional).
- **RabbitMQ**: Message broker (optional).
- **Elasticsearch/OpenSearch**: For search functionality (optional).
- **Mailpit**: To capture emails sent during development (optional).

## Development

If you want to contribute to Orobox, you can use the provided `Makefile` to simplify common development tasks.

### Prerequisites
- **Go** (version 1.25.0 or later)
- **golangci-lint** (version 1.64.0 or later recommended)

### Available Commands
- **Run Linting**:
  ```bash
  make lint
  ```
- **Run Tests**:
  ```bash
  make test
  ```
- **Build Locally**:
  ```bash
  make build
  ```

---
Developed to simplify the work of OroCommerce developers.
