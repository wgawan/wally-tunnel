# wally-tunnel

> **Early Release**: This project is functional and actively used, but it is still early. Read [SECURITY.md](SECURITY.md) before putting it on the internet.

A self-hosted reverse tunnel for one developer.
It is an `ngrok`-style tool, similar in spirit to `Cloudflare Tunnel` and other hosted tunneling providers, but designed to run on your own VPS with a simpler single-user trust model.

Use it to:
- open a local app on your own phone or tablet
- send a teammate a quick demo URL
- expose a temporary preview from your laptop through your own VPS

Do not use it as:
- a multi-user tunnel platform
- a permanent shared environment
- a secure URL generator
- a replacement for authentication inside your app

If you want a self-hosted `ngrok` alternative, a simple reverse tunnel on your own domain, or an easy way to expose `localhost` from your laptop, that is where wally-tunnel fits.

## What You Get

- HTTP/HTTPS request proxying through a WebSocket tunnel
- WebSocket proxying with subprotocol support, including Vite HMR
- Server-Sent Events and streaming response support
- Separate HTTP and WebSocket ports per subdomain
- Automatic TLS via Caddy
- Automatic client reconnects
- YAML config file plus CLI flags and environment variables

## Security In One Minute

- Tunnel URLs are public internet URLs.
- The tunnel does not protect your app from end users. Your app's auth is still your job.
- The client token is the only credential for registering tunnels. Treat it like a password.
- This project is intended for a single operator using their own tunnel server.
- Use a dedicated subdomain such as `tunnel.example.dev`, not your root domain.

More detail: [SECURITY.md](SECURITY.md)

## Quick Start

### 1. Prepare a VPS and DNS

You need:
- a VPS with a public IP
- a domain or subdomain you control

Recommended setup:

- Base domain for the tunnel: `tunnel.example.dev`
- Public tunnel URLs: `app.tunnel.example.dev`, `api.tunnel.example.dev`

Create a wildcard DNS record:

```text
  tunnel.example.dev  ->  A  ->  <your-vps-ip>
*.tunnel.example.dev  ->  A  ->  <your-vps-ip>
```

Using a dedicated subdomain limits blast radius and keeps tunnel traffic separate from the rest of your domain.

### 2. Set Up the Server

On the VPS:

```bash
git clone https://github.com/wgawan/wally-tunnel.git
cd wally-tunnel
sudo WALLY_TUNNEL_DOMAIN=tunnel.example.dev bash deploy/setup.sh
```

Then harden it immediately:

```bash
sudo bash deploy/harden.sh
```

The setup flow:
- installs the latest server binary
- installs Caddy for TLS
- generates a random auth token
- creates the systemd service
- prints the client config you need

The hardening script:
- restricts incoming traffic to `22`, `80`, and `443`
- enables key-only SSH
- installs `fail2ban`
- applies kernel hardening

### 3. Install the Client

```bash
curl -fsSL https://raw.githubusercontent.com/wgawan/wally-tunnel/master/install.sh | bash
```

Or with Go:

```bash
go install github.com/wgawan/wally-tunnel/cmd/wally-tunnel@latest
```

### 4. Create Your Config

Create `~/.wally-tunnel.yaml`:

```yaml
server: tunnel.example.dev
token: your-token-from-setup
domain: tunnel.example.dev
mappings:
  app: 3000
```

You can add lightweight tunnel-level guardrails when you share a demo:

```yaml
server: tunnel.example.dev
token: your-token-from-setup
domain: tunnel.example.dev
mappings:
  app:
    http: 3000
    protect:
      basic_auth:
        username: demo
        password: ${APP_DEMO_PASSWORD}
      expires_in: 2h
```

### 5. Run It

```bash
wally-tunnel
```

Now your local service on `localhost:3000` is reachable at:

```text
https://app.tunnel.example.dev
```

This is the intended workflow:
- start a tunnel
- use it briefly for your own device testing or a teammate demo
- stop it when you are done

## Recommended Usage

Good fits:
- checking a local app on mobile hardware
- showing a teammate a short-lived demo
- temporarily exposing a dev server with its own auth

Bad fits:
- long-lived shared environments
- sensitive internal tools without authentication
- anything that assumes the URL itself is secret
- sharing the client token with multiple people

## Security Checklist

Before you use it on the internet:

- Use a dedicated subdomain such as `tunnel.example.dev`.
- Run `deploy/harden.sh` on the VPS.
- Verify only ports `22`, `80`, and `443` are publicly reachable.
- Keep the tunnel backend on `:8080` inaccessible from the public internet.
- Treat the client token like a password and rotate it if it leaks.
- Only expose apps that are safe to make public or already enforce auth.
- Stop the client when the demo or testing session is over.
- Prefer `basic_auth` and `expires_in` for anything you share with other people.

## Advanced Configuration

### Multiple subdomains

```yaml
mappings:
  app: 5173
  api: 3000
```

This exposes:
- `https://app.tunnel.example.dev`
- `https://api.tunnel.example.dev`

### Lightweight guardrails

Each mapping can optionally add basic auth and an automatic expiry:

```yaml
mappings:
  app:
    http: 3000
    protect:
      basic_auth:
        username: demo
        password: ${APP_DEMO_PASSWORD}
      expires_in: 4h
```

This keeps the tunnel easy to start while giving shared demos a safer default posture.

### Separate WebSocket port

If your app serves HTTP and WebSocket traffic on different ports:

```yaml
mappings:
  app:
    http: 5173
    ws: 64999
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `WALLY_TUNNEL_TOKEN` | Auth token if not provided via config or CLI |

### Configuration precedence

CLI flags > YAML config file > environment variables

## Server Requirements

- 1 vCPU
- 512 MB RAM
- Ubuntu 22.04+ or similar Debian-based system
- a public IP
- a domain you control

Any VPS provider works.

## Deployment Notes

The `deploy/` directory contains:

- `setup.sh`: installs the server, Caddy, token, and service
- `harden.sh`: applies host hardening for the expected use case
- `Caddyfile`: TLS and reverse proxy config
- `wally-tunnel-server.service`: systemd unit for the tunnel server

Important deployment note:

- The tunnel server listens on `:8080` behind Caddy.
- Do not leave `:8080` publicly reachable.
- The intended deployment is a hardened host where only `22`, `80`, and `443` are exposed.

## How It Works

1. The client connects to your VPS over WebSocket and authenticates with a shared token.
2. The client registers one or more subdomains.
3. Your VPS receives requests for those subdomains.
4. The server forwards those requests through the tunnel to your laptop.
5. Your laptop sends the response back through the tunnel.

## License

[MIT](LICENSE)
