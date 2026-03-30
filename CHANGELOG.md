# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] - 2024-12-01

### Added

- HTTP/HTTPS request proxying through WebSocket tunnel
- WebSocket proxying with subprotocol support (e.g., Vite HMR)
- Server-Sent Events (SSE) and streaming response support
- Separate port routing for WebSocket vs HTTP traffic per subdomain
- Automatic TLS via Caddy on-demand certificates
- Automatic reconnection with exponential backoff
- YAML config file, CLI flags, and environment variable configuration
- Automated VPS setup script with Caddy and systemd
- GitHub Actions CI with race detection
