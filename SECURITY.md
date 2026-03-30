# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do not open a public issue.**

Instead, email **security@wgawan.com** with:

- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You should receive a response within 72 hours. We'll work with you to understand the issue and coordinate a fix before any public disclosure.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Security Considerations

wally-tunnel is designed for self-hosted use. Keep in mind:

- **Token security**: The shared authentication token is stored in plaintext in the server env file and client config. Protect these files with appropriate filesystem permissions.
- **Network exposure**: The tunnel server accepts connections from any client with a valid token. Use firewall rules to restrict access if needed.
- **No rate limiting**: There is currently no built-in rate limiting on authentication attempts. Consider using Caddy's rate limiting or a firewall.
