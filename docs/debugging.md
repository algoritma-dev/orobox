[← Back to README](../README.md) · [Documentation index](README.md)

# Debugging with Xdebug

Orobox includes Xdebug preinstalled, but disabled by default to maintain performance.

### 1. Enabling/Disabling Xdebug

You can enable or disable Xdebug in currently running containers using the `xdebug` command:

```bash
orobox xdebug enable
```

This command will:
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

*Note: Since the configuration is not persistent, Xdebug will be disabled again after a container restart or `orobox up`.*

### 2. Xdebug for CLI, Consumer, and Cron
For debugging background processes or to have Xdebug enabled by default, you can set these variables in your `.env` file:
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
    - **Local Path**: The root folder of your bundle on the host machine.
    - **Remote Path**: `/var/www/oro/src/<BundleNamespace>` (e.g., `/var/www/oro/src/MyVendor/Bundle/MyBundle`).
4.  **Xdebug Port**: Ensure the port in `Settings -> PHP -> Debug` is set to `9003`.
