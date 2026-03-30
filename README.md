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

```bash
# On your VPS
git clone https://github.com/wgawan/wally-tunnel.git
cd wally-tunnel

# Point *.yourdomain.dev DNS to your VPS IP (wildcard A record)

sudo WALLY_TUNNEL_DOMAIN=yourdomain.dev bash deploy/setup.sh
# Save the token it prints — you'll need it on your laptop
```

### 2. Install the client (laptop)

```bash
go install github.com/wgawan/wally-tunnel/cmd/wally-tunnel@latest
```

Or build from source:

```bash
git clone https://github.com/wgawan/wally-tunnel.git
cd wally-tunnel
make build
# Binary at ./bin/wally-tunnel
```

### 3. Connect

**Option A: Config file** (recommended)

Create `~/.wally-tunnel.yaml`:

```yaml
server: tunnel.yourdomain.dev
token: your-secret-token
domain: yourdomain.dev
mappings:
  app: 5173
  api: 3000
```

Then just run:

```bash
wally-tunnel
```

**Option B: CLI flags**

```bash
wally-tunnel \
  -server tunnel.yourdomain.dev \
  -token YOUR_TOKEN \
  -domain yourdomain.dev \
  -map app:5173 \
  -map api:3000
```

Your services are now accessible at `https://app.yourdomain.dev` and `https://api.yourdomain.dev`.

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

- `setup.sh` — Automated setup script (installs Caddy, generates token, configures systemd)
- `Caddyfile` — Caddy config with on-demand TLS for wildcard subdomains
- `wally-tunnel-server.service` — systemd unit file

**DNS requirement:** Create a wildcard A record `*.yourdomain.dev` pointing to your VPS IP.

**Manual server deploy:**

```bash
# Build and deploy the server binary
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
