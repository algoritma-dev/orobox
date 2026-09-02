[← Back to README](../README.md) · [Documentation index](README.md)

# Domains, SSL and WebSocket

## Domains and SSL
- **Hosts File**: For each domain defined in `.orobox.yaml`, you must add an entry to your system's `hosts` file (e.g., `/etc/hosts` on Linux/macOS or `C:\Windows\System32\drivers\etc\hosts` on Windows) pointing to `127.0.0.1`.
  Example: `127.0.0.1 oro.demo`
- **SSL with mkcert**: If you enable SSL, Orobox uses `mkcert` to generate local certificates. Ensure `mkcert` is installed and you have run `mkcert -install` once on your machine.

## WebSocket

The Oro websocket server (`gos:websocket:server`) runs in the `ws` service and is never
published on the host: nginx proxies the `/ws` location of every configured domain to it and
performs the HTTP upgrade, so the browser connects to the same origin it is already browsing
(`wss://` on an SSL domain, `ws://` otherwise).

Orobox owns the addressing and injects it into `php-fpm-app`, `ws`, `consumer` and `cron`, so
a project checkout carrying its own websocket settings in `.env-app.local` cannot point them
at a host that does not exist inside the Compose network:

- `ORO_WEBSOCKET_SERVER_DSN=//0.0.0.0:8080` — what the server binds to.
- `ORO_WEBSOCKET_BACKEND_DSN=tcp://ws:8080` — how PHP reaches the server, direct, without nginx.
- `ORO_WEBSOCKET_FRONTEND_DSN=//*:<published nginx port>/ws` — what the browser is told to use.

The frontend port is the published nginx port (`nginx_https_port` when at least one domain uses
SSL, `nginx_http_port` otherwise). Only one port can be advertised, so a configuration that mixes
SSL and plain domains advertises the HTTPS one and the plain domains fall back to no live updates.

Websocket logs come from `orobox logs --ws`. The connection is not required for the application
to work: when the server is down, Oro simply stops rendering the client-side sync code.
