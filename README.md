# Orobox - CLI Tool for OroCommerce Development

Orobox is a command-line tool (CLI) developed in Go to quickly set up an isolated and reproducible development environment for OroCommerce. It supports the development of individual **bundles**, the entire **application project**, or the creation of a **local demo/production** environment.

## ⚠️ Important Disclaimer
**WARNING: This tool is designed EXCLUSIVELY for local development. It MUST NOT be used in production environments.** Orobox configures the environment to facilitate debugging and development, which may not comply with security requirements and best practices necessary for a production environment.

## Prerequisites
Before installing Orobox, make sure you have installed on your system:
- **Docker** and **Docker Compose**

## Installation

### Linux / macOS

Run the following command in your terminal. It will automatically detect your operating system and architecture:

```bash
curl -sSfL "https://github.com/algoritma-dev/orobox/releases/download/0.0.9-dev/orobox_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/')" -o ~/.local/bin/orobox && chmod +x ~/.local/bin/orobox && hash -r
```

*Note: Make sure `~/.local/bin` is in your `PATH`.*

### Windows (PowerShell)

Run the following command in PowerShell:

```powershell
mkdir -Force "$HOME\.local\bin"; iwr "https://github.com/algoritma-dev/orobox/releases/download/0.0.9-dev/orobox_Windows_$($env:PROCESSOR_ARCHITECTURE.ToLower().Replace('amd64','x86_64')).exe" -OutFile "$HOME\.local\bin\orobox.exe"
```

*Note: Make sure to add `%USERPROFILE%\.local\bin` to your User `Path` environment variable.*

## Configuration (`.orobox.yaml`)

Orobox uses a configuration file called `.orobox.yaml` in the root of your bundle or project. If the file does not exist, the `init` command will guide you through its interactive creation.

Example `.orobox.yaml` file:
```yaml
type: bundle # Can be: bundle, project, demo
class: MyBundle
namespace: MyVendor\Bundle\MyBundle
oro_version: "6.1"
domains:
  - host: oro.demo
    root: public
    ssl: false
services:
  redis: true
  redisinsight: true
  mailpit: true
  rabbitmq: true
  elasticsearch: false
  kibana: false
  adminer: true
test:
  use_tmpfs: true
  tmpfs_size: 1g
```

### Configuration Fields
- `type`: Defines the installation type.
    - `bundle` (default): Optimized for developing a single bundle. Maps local code to `/var/www/oro/src/<Namespace>`.
    - `project`: For developing an entire OroCommerce application. Maps the entire local project to `/var/www/oro`.
    - `demo`: Environment similar to production (`ORO_ENV=prod`). Xdebug, Mailpit, and other dev tools are disabled.
- `class`: (Only for `type: bundle`) Name of the bundle class.
- `namespace`: (Only for `type: bundle`) PHP namespace of the bundle.
- `oro_version`: OroCommerce version (e.g., "7.0", "6.1", "6.0", "5.1").
- `domains`: List of domains for the environment.
- `services`: Configuration for optional services and tools:
    - `redis`: (bool) Enable/disable Redis.
    - `redisinsight`: (bool) Enable/disable RedisInsight.
    - `mailpit`: (bool) Enable/disable Mailpit.
    - `rabbitmq`: (bool) Enable/disable RabbitMQ.
    - `elasticsearch`: (bool) Enable/disable Elasticsearch/OpenSearch.
    - `kibana`: (bool) Enable/disable Kibana (only if Elasticsearch is enabled).
    - `adminer`: (bool) Enable/disable Adminer (PostgreSQL manager).
- `test`:
    - `use_tmpfs`: (bool) If enabled, uses RAM (tmpfs) for database files in the test container, significantly improving performance but data is lost on container restart.
    - `tmpfs_size`: (string) Size of the tmpfs mount (e.g., "1g", "512m").

*Note: Versions of PHP, PostgreSQL, Node.js, and other components are automatically determined by the `oro_version` setting and cannot be changed manually.*

### Global Flags
These options can be used with any command:
- `--config`: Specifies an alternative configuration file (default: `.orobox.yaml`).

## Command Usage

The main command is `oro` (or `orobox`, depending on how you installed it).

### 1. Initialization (`init`)
Prepares the development environment in your bundle or project repository.
```bash
orobox init
```
This command:
- Creates the `.orobox.yaml` file if missing (interactive mode).
- Generates SSL certificates if required.
- Configures the necessary Docker files.

Options:
- `--type`, `-t`: Installation type (`bundle`, `project`, `demo`).
- `--bundle-path`, `-b`: Bundle path (default ".").
- `--oro-version`, `-v`: OroCommerce version to use (default "6.1").
- `--bundle-namespace`, `-n`: Bundle namespace (e.g., "MyVendor/Bundle/MyBundle").

### 2. Start Environment (`up`)
Starts Docker containers and configures OroCommerce.
```bash
orobox up
```
The command dynamically generates the `docker-compose.yml` file, starts the services, and proceeds with the environment installation or update.

### 3. Stop Environment (`down`)
Shuts down the Docker services associated with the project.
```bash
orobox down
```

### 4. Shell Access (`shell`)
Accesses a container in interactive mode (default: php).
```bash
orobox shell
```

### 5. View Logs (`logs`)
Displays logs from different services in the development environment. At least one flag must be specified.
```bash
orobox logs --app
```
Options:
- `--nginx`: Nginx logs.
- `--php`: PHP-FPM logs.
- `--app`: Symfony/OroCommerce logs.
- `--consumer`: Consumer logs.
- `--cron`: Cron logs.
- `--ws`: WebSocket logs.

### 6. Symfony Console (`console`)
Executes Symfony commands in the application container.
```bash
orobox console cache:clear
```

### 7. Run Tests (`test`)
Runs PHPUnit tests within the configured environment.
```bash
orobox test
```

### 8. QA Tools Initialization (`qa-init`)
Configures and installs the necessary QA tools (PHPStan, coding standards, ESLint, Stylelint) in your bundle or project.
```bash
orobox qa-init
```
*Note: This command is not available in `type: demo` mode.*

### 9. Run QA Tools (`qa`)
Executes the QA analysis tools. By default, it runs all tools if no specific flag is provided.
```bash
orobox qa
```
Options:
- `--phpstan`: Run PHPStan analysis.
- `--rector`: Run Rector process.
- `--php-cs-fixer`: Run PHP-CS-Fixer fix.
- `--twig-cs-fixer`: Run Twig-CS-Fixer lint.
- `--eslint`: Run ESLint analysis.
- `--stylelint`: Run Stylelint analysis.

Example:
```bash
orobox qa --phpstan --eslint
```
*Note: This command is not available in `type: demo` mode.*

### 10. Total Cleanup (`clean`)
Removes all associated containers and volumes to start from scratch.
```bash
orobox clean
```

## Debugging with Xdebug

Orobox includes Xdebug preinstalled, but disabled by default to maintain performance.

**Note:** Xdebug is always disabled in `type: demo` mode.

### 1. Enabling/Disabling Xdebug

You can enable or disable Xdebug using the `xdebug` command:

```bash
orobox xdebug enable
```

This command will:
- Update your `.orobox.yaml` configuration.
- Regenerate the necessary Docker files.
- Apply the change immediately to running containers ("hot-patching").

To disable Xdebug:
```bash
orobox xdebug disable
```

You can specify which environment to target:
```bash
orobox xdebug enable --dev   # Development environment (default)
orobox xdebug enable --test  # Test environment
```

### 2. Manual Configuration (Optional)
If you prefer, you can still manually enable Xdebug in `.orobox.yaml`:

```yaml
services:
  php:
    xdebug: true
```

After changing this setting, run `orobox up` to apply the configuration.

### 3. Xdebug for CLI, Consumer, and Cron
By default, enabling Xdebug in `.orobox.yaml` activates it for FPM (web requests) and interactive CLI commands (e.g., `orobox console`). 

For debugging background processes, you can manually set these variables in your `.env` file:
- `ORO_CONSUMER_XDEBUG_ENABLED=true`: For Message Queue consumers.
- `ORO_CRON_XDEBUG_ENABLED=true`: For Cron jobs.

After updating the `.env` file, restart the environment with `orobox up`.

### 3. PHPStorm Configuration
To debug with PHPStorm:
1.  **Listen for Debug Connections**: Ensure the 'Phone' icon (Start Listening for PHP Debug Connections) is ON.
2.  **Server Configuration**: Go to `Settings -> PHP -> Servers` and add a new server:
    - **Host**: Your domain (default: `oro.demo` or your custom domain in `.orobox.yaml`).
    - **Port**: `80` (or `443` if using SSL).
    - **Debugger**: Xdebug.
3.  **Path Mappings**: Enable "Use path mappings" and configure:
    - **Local Path**: The root folder of your bundle or project on the host machine.
    - **Remote Path**: 
        - For `type: bundle`: `/var/www/oro/src/<BundleNamespace>` (e.g., `/var/www/oro/src/MyVendor/Bundle/MyBundle`).
        - For `type: project`: `/var/www/oro`.
4.  **Xdebug Port**: Ensure the port in `Settings -> PHP -> Debug` is set to `9003`.

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
- **Go** (version 1.25 or later)
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

---

Developed to simplify the work of OroCommerce developers.

---

### Inspired by
- [MageBox](https://magebox.dev/)
