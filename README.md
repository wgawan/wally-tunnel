# wally-tunnel

> **Early Release**: This project was recently open-sourced. While it is functional and actively used, it has not been widely tested across environments. Please review the [security documentation](SECURITY.md) before deploying, and report any issues you find.

A self-hosted reverse tunnel that exposes local services through custom subdomains on your own VPS. Think ngrok, but you own the infrastructure.

```
┌──────────────┐       ┌──────────────────┐       ┌──────────────┐
│  Browser     │──────▶│  VPS (server)    │◀──────│  Your laptop │
│              │ HTTPS │  Caddy + tunnel  │  WSS  │  (client)    │
│ app.your.dev │       │  server on :8080 │       │  localhost:   │
│              │◀──────│                  │──────▶│    3000,5173  │
└──────────────┘       └──────────────────┘       └──────────────┘
```

**Features:**
- HTTP/HTTPS request proxying through WebSocket tunnel
- WebSocket proxying with subprotocol support (e.g., Vite HMR)
- Server-Sent Events (SSE) and streaming response support
- Separate port routing for WebSocket vs HTTP traffic per subdomain
- Automatic TLS via Caddy on-demand certificates
- Automatic reconnection with exponential backoff
- YAML config file + CLI flags + environment variables

## Quick Start

### 1. Set up the server (VPS)

Point `*.yourdomain.dev` DNS to your VPS IP (wildcard A record), then:

```bash
# SSH into your VPS
git clone https://github.com/wgawan/wally-tunnel.git
cd wally-tunnel
sudo WALLY_TUNNEL_DOMAIN=yourdomain.dev bash deploy/setup.sh
```

The setup script will:
- Download the latest server binary from GitHub Releases
- Install and configure Caddy for automatic TLS
- Generate a secure auth token
- Create a sandboxed systemd service
- Print your client config with the token and next steps

### 2. Install the client (laptop)

```bash
curl -fsSL https://raw.githubusercontent.com/wgawan/wally-tunnel/master/install.sh | bash
```

Or with Go:

```bash
go install github.com/wgawan/wally-tunnel/cmd/wally-tunnel@latest
```

### 3. Connect

Create `~/.wally-tunnel.yaml` (the setup script prints this for you):

```yaml
server: tunnel.yourdomain.dev
token: your-token-from-setup
domain: yourdomain.dev
mappings:
  app: 3000
```

Then run:

```bash
wally-tunnel
```

Your service is now live at `https://app.yourdomain.dev`.

You can map multiple subdomains:

```yaml
mappings:
  app: 5173
  api: 3000
```

This gives you `https://app.yourdomain.dev` and `https://api.yourdomain.dev`.

## Advanced Configuration

### Separate WebSocket port

If your app serves WebSocket traffic on a different port (e.g., Vite HMR on 64999):

```yaml
mappings:
  app:
    http: 5173
    ws: 64999
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `WALLY_TUNNEL_TOKEN` | Auth token (fallback if not in config/CLI) |

### Configuration precedence

CLI flags > YAML config file > environment variables

## Server Deployment

The `deploy/` directory contains everything for a VPS setup:

- `setup.sh` — Automated setup (downloads binary, installs Caddy, generates token, configures systemd)
- `Caddyfile` — Caddy config with on-demand TLS for wildcard subdomains
- `wally-tunnel-server.service` — Sandboxed systemd unit (runs as `wally-tunnel` user)

**DNS requirement:** Create a wildcard A record `*.yourdomain.dev` pointing to your VPS IP.

**Updating the server:**

```bash
# Re-run setup.sh to download the latest release (keeps your existing token)
sudo bash deploy/setup.sh
```

Or deploy a custom build:

```bash
make deploy VPS_IP=your-vps-ip VPS_USER=root
```

## Architecture

The system uses a WebSocket-based tunnel protocol:

1. **Client** connects to server via WebSocket and authenticates with a shared token
2. **Client** registers subdomain mappings (e.g., "app" -> localhost:5173)
3. **Server** receives HTTP requests for `app.yourdomain.dev`, wraps them in the tunnel protocol, and forwards them to the client
4. **Client** makes the request to the local service and sends the response back through the tunnel
5. **WebSocket** and **SSE** connections are proxied with streaming support

## License

[MIT](LICENSE)
