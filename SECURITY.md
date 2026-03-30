# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do not open a public issue.**

Instead, email **wally@stackjive.com** with:

- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You should receive a response within 72 hours. We'll work with you to understand the issue and coordinate a fix before any public disclosure.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Security Model

wally-tunnel is a self-hosted tunneling tool. By design, it exposes local services to the public internet through your server. The security boundary is:

- **Authentication**: A shared token authenticates tunnel clients to the server. Only clients with the correct token can register subdomains.
- **No encryption between client and server**: TLS terminates at Caddy (the reverse proxy). The tunnel WebSocket connection between your client and server should always run behind a TLS-terminating proxy.
- **No tenant isolation**: All authenticated clients share the same server. Anyone with the token can register any available subdomain.

## Built-in Protections

The following protections are implemented in the application:

### Rate Limiting
Per-IP rate limiting (5 attempts/minute) on tunnel WebSocket connection attempts to mitigate brute-force token guessing. Returns HTTP 429 when exceeded.

### Subdomain Validation
Subdomain names are validated against a strict pattern (alphanumeric + hyphens, max 63 chars) and reserved names (`_tunnel`, `www`, `api`, etc.) are rejected to prevent internal endpoint hijacking.

### Error Sanitization
Error messages returned to external clients through the tunnel are sanitized to prevent leaking internal state (server paths, stack traces, internal IPs). Detailed errors are logged server-side only.

### Concurrent Write Safety
WebSocket writes from the tunnel client are serialized with a mutex to prevent corrupt frames from concurrent goroutines.

### Internal Endpoint Restriction
The `_tunnel/check` endpoint (used by Caddy's on-demand TLS) is restricted to loopback addresses (127.0.0.1/::1) to prevent external enumeration of registered subdomains.

### Token Handling
- The setup script sets `0600` permissions on the token env file so only root can read it.
- A warning is logged if the token is passed via the `-token` CLI flag, since command-line arguments are visible in `/proc/<pid>/cmdline` and shell history. Use the `WALLY_TUNNEL_TOKEN` environment variable or a config file instead.

## Deployment Hardening Recommendations

When self-hosting, the server should be locked down beyond the application defaults. These are not enforced by the software but are strongly recommended:

### SSH
- Disable password authentication — use key-based auth only.
- Set `PermitRootLogin prohibit-password` (or create a non-root user and set `PermitRootLogin no`).
- Disable X11 forwarding, TCP forwarding, and agent forwarding.
- Set `MaxAuthTries 3` to limit brute-force attempts.

### Firewall
- Allow only the ports you need: `22` (SSH), `80` (HTTP redirect), `443` (HTTPS).
- Deny all other incoming traffic by default.

### Intrusion Prevention
- Install `fail2ban` with aggressive SSH jail settings to auto-ban IPs after repeated failed login attempts.

### Service Sandboxing
- Run `wally-tunnel-server` as a dedicated non-root user (e.g., `wally-tunnel`).
- Use systemd sandboxing directives:
  ```ini
  [Service]
  User=wally-tunnel
  Group=wally-tunnel
  NoNewPrivileges=true
  ProtectSystem=strict
  ProtectHome=true
  PrivateTmp=true
  PrivateDevices=true
  ProtectKernelTunables=true
  ProtectKernelModules=true
  ProtectControlGroups=true
  RestrictSUIDSGID=true
  RestrictNamespaces=true
  MemoryDenyWriteExecute=true
  RestrictRealtime=true
  SystemCallArchitectures=native
  ReadOnlyPaths=/etc/wally-tunnel
  ```
- Set the env file permissions to `640 root:<service-user>` so only the service can read the token.

### Kernel Hardening
Apply sysctl settings to reduce attack surface:
- Enable SYN flood protection (`tcp_syncookies`)
- Disable ICMP redirects and source routing
- Disable IP forwarding (unless needed)
- Restrict unprivileged BPF and ptrace
- Hide kernel pointers (`kptr_restrict = 2`)

### TLS
- Always run behind a TLS-terminating reverse proxy (Caddy, nginx, etc.).
- The included Caddy configuration uses on-demand TLS with Let's Encrypt, providing automatic certificate provisioning for registered subdomains.

## Security Considerations

- **Token is a single shared secret**: Anyone with the token has full access. Rotate it periodically and treat it like a password.
- **Subdomains are public**: Once a tunnel client registers a subdomain, it is accessible to anyone on the internet. Only expose services you intend to be public.
- **No request-level auth**: The tunnel itself does not authenticate end-user HTTP requests. Your tunneled application is responsible for its own authentication and authorization.
- **Log monitoring**: Monitor server logs for unexpected tunnel registrations or repeated authentication failures, which may indicate token compromise or brute-force attempts.
