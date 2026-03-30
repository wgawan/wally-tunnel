# Security Policy

## Reporting a Vulnerability

If you discover a security issue, do not open a public issue.

Email **wally@stackjive.com** with:
- a description of the issue
- steps to reproduce
- potential impact
- a suggested fix, if you have one

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Intended Security Posture

wally-tunnel is designed for a narrow use case:

- one developer operating their own tunnel server
- temporary access to a local service
- personal device testing or quick teammate demos

It is not designed for:
- multi-tenant access
- permanent shared environments
- secret or hard-to-guess URLs as a security control
- replacing authentication and authorization in the app you expose

If you need long-lived access, multi-user permissions, audit controls, or URL secrecy, this is the wrong tool.

## Trust Model

The trust model is intentionally simple:

- A single shared token authenticates tunnel clients to the server.
- Anyone with that token can register available subdomains.
- End-user HTTP requests are not authenticated by the tunnel.
- Registered subdomains are public internet endpoints.
- TLS is expected to terminate at a reverse proxy such as Caddy.

Operationally, this means:

- protect the token like a password
- do not share the token with teammates
- only expose apps that are safe to make public or already have their own auth
- stop the tunnel when you no longer need it

## What The Software Protects

The application includes a small set of built-in protections:

### Client Authentication

Tunnel clients must authenticate with the shared token before they can register subdomains.

### Subdomain Validation

Subdomains are restricted to lowercase alphanumeric names with optional hyphens. Reserved names such as `www`, `api`, and `_tunnel` are rejected.

### Connection Attempt Rate Limiting

Tunnel connection attempts are rate limited to reduce brute-force guessing of the shared token.

### Error Sanitization

Errors returned through the tunnel are sanitized to avoid leaking internal paths, stack traces, or local network details to internet clients.

### WebSocket Write Serialization

Tunnel WebSocket writes are serialized to avoid concurrent frame corruption.

### Loopback Restriction For Internal TLS Checks

The internal `/_tunnel/check` endpoint is restricted to loopback addresses. It is meant for the reverse proxy's on-demand TLS check, not for public use.

## What The Software Does Not Protect

These are explicit non-goals:

### Public URL Access

If a subdomain is active, anyone on the internet can send requests to it.

### Request-Level Auth

The tunnel does not add login, session checks, ACLs, or per-request authorization.

### Tenant Isolation

This is not a shared platform. Anyone with the token can act as a trusted tunnel client.

### Secret URL Security

This project does not generate unguessable or security-sensitive URLs. A readable subdomain like `app.example.dev` is normal and expected.

### Long-Term Exposure Safety

This tool is optimized for temporary access, not permanent internet exposure.

## Recommended Deployment

For the intended use case, the safest deployment looks like this:

- use a dedicated tunnel subdomain such as `tunnel.example.dev`
- point `*.tunnel.example.dev` at your VPS
- terminate TLS with Caddy or another reverse proxy
- expose only ports `22`, `80`, and `443` to the public internet
- keep the tunnel backend on `:8080` unreachable from the public internet
- run the server as a dedicated non-root user
- keep the auth token in a root-owned env file

The included `deploy/harden.sh` script is part of that posture. Run it before you start depending on the service.

## Operational Guidance

### Treat The Token Like A Password

The token is the only credential for tunnel client registration. If it leaks, rotate it.

### Prefer A Dedicated Subdomain

Use `tunnel.example.dev`, not `example.dev`, so tunnel traffic stays isolated from your main domain.

### Expose Only What You Intend To Be Public

If the local app should not be public without a login, add a login to the app before exposing it.

### Keep Demos Short-Lived

Start the tunnel when you need it. Stop it when you are done.

### Monitor Logs

Watch for unexpected registrations or repeated failed tunnel connections.

## Notes On TLS

The recommended deployment uses Caddy with on-demand TLS.

Important nuance:

- the tunnel server's internal TLS authorization endpoint is loopback-only
- certificate provisioning is scoped to first-level subdomains under your configured tunnel domain
- TLS gives transport security for the public URL, but it does not make the URL private or access-controlled

## Summary

The correct mental model is:

- this is a secure transport path to a public URL
- it is not an access-control system
- it is best for one operator doing temporary sharing or device testing

That is the security posture this project is designed to maximize.
