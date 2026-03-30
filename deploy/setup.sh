#!/usr/bin/env bash
set -euo pipefail

echo "=== wally-tunnel-server setup ==="

# 0. Get the domain from the user (or env var)
WALLY_TUNNEL_DOMAIN="${WALLY_TUNNEL_DOMAIN:-}"
if [ -z "$WALLY_TUNNEL_DOMAIN" ]; then
    read -rp "Enter your tunnel domain (e.g., yourdomain.dev): " WALLY_TUNNEL_DOMAIN
fi
if [ -z "$WALLY_TUNNEL_DOMAIN" ]; then
    echo "Error: domain is required. Set WALLY_TUNNEL_DOMAIN or pass it interactively."
    exit 1
fi
echo "Using domain: $WALLY_TUNNEL_DOMAIN"

# 1. Install Caddy (stock - no plugins needed)
if ! command -v caddy &> /dev/null; then
    echo "Installing Caddy..."
    sudo apt-get update
    sudo apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
    sudo apt-get update
    sudo apt-get install -y caddy
fi

# 2. Configure Caddy
sudo mkdir -p /etc/caddy
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy

# 3. Create service user
if ! id -u wally-tunnel &>/dev/null; then
    echo "Creating wally-tunnel service user..."
    sudo useradd -r -s /usr/sbin/nologin -d /nonexistent wally-tunnel
fi

# 4. Create env file for wally-tunnel-server
sudo mkdir -p /etc/wally-tunnel
if [ ! -f /etc/wally-tunnel/env ]; then
    TOKEN=$(openssl rand -base64 32)
    echo ""
    echo "========================================"
    echo "  Your tunnel token: $TOKEN"
    echo "  Save this! You'll need it on your laptop."
    echo "========================================"
    echo ""
    sudo tee /etc/wally-tunnel/env > /dev/null <<ENV
WALLY_TUNNEL_TOKEN=$TOKEN
WALLY_TUNNEL_DOMAIN=$WALLY_TUNNEL_DOMAIN
ENV
fi
sudo chmod 640 /etc/wally-tunnel/env
sudo chown root:wally-tunnel /etc/wally-tunnel/env

# 5. Install wally-tunnel-server service
sudo cp deploy/wally-tunnel-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now wally-tunnel-server

echo ""
echo "=== Setup complete ==="
echo ""
echo "Services running:"
echo "  - caddy (TLS termination on :443)"
echo "  - wally-tunnel-server (tunnel backend on :8080)"
echo ""
