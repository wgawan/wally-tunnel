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

# 1. Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# 2. Download the latest server binary from GitHub Releases
BINARY_NAME="wally-tunnel-server-linux-${GOARCH}"
echo "Downloading latest wally-tunnel-server (${GOARCH})..."
DOWNLOAD_URL=$(curl -fsSL https://api.github.com/repos/wgawan/wally-tunnel/releases/latest \
    | grep "browser_download_url.*${BINARY_NAME}" \
    | head -1 \
    | cut -d '"' -f 4)

if [ -z "$DOWNLOAD_URL" ]; then
    echo "Error: could not find a release binary for ${BINARY_NAME}."
    echo "You may need to build from source: make build-linux"
    exit 1
fi

curl -fsSL -o /tmp/wally-tunnel-server "$DOWNLOAD_URL"
sudo install -m 755 /tmp/wally-tunnel-server /usr/local/bin/wally-tunnel-server
rm -f /tmp/wally-tunnel-server
echo "Installed wally-tunnel-server to /usr/local/bin/"

# 3. Install Caddy (stock - no plugins needed)
if ! command -v caddy &> /dev/null; then
    echo "Installing Caddy..."
    sudo apt-get update -qq
    sudo apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
    sudo apt-get update -qq
    sudo apt-get install -y -qq caddy
fi

# 4. Configure Caddy
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
sudo mkdir -p /etc/caddy
sudo cp "${SCRIPT_DIR}/Caddyfile" /etc/caddy/Caddyfile
sudo systemctl restart caddy

# 5. Create service user
if ! id -u wally-tunnel &>/dev/null; then
    echo "Creating wally-tunnel service user..."
    sudo useradd -r -s /usr/sbin/nologin -d /nonexistent wally-tunnel
fi

# 6. Create env file for wally-tunnel-server
sudo mkdir -p /etc/wally-tunnel
if [ ! -f /etc/wally-tunnel/env ]; then
    TOKEN=$(openssl rand -base64 32)
    sudo tee /etc/wally-tunnel/env > /dev/null <<ENV
WALLY_TUNNEL_TOKEN=$TOKEN
WALLY_TUNNEL_DOMAIN=$WALLY_TUNNEL_DOMAIN
ENV
else
    TOKEN=$(grep WALLY_TUNNEL_TOKEN /etc/wally-tunnel/env | cut -d= -f2)
fi
sudo chmod 640 /etc/wally-tunnel/env
sudo chown root:wally-tunnel /etc/wally-tunnel/env

# 7. Install and start systemd service
sudo cp "${SCRIPT_DIR}/wally-tunnel-server.service" /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now wally-tunnel-server

echo ""
echo "=== Setup complete ==="
echo ""
echo "Server is running on ${WALLY_TUNNEL_DOMAIN}"
echo ""
echo "Next steps:"
echo ""
echo "  1. Make sure DNS is configured:"
echo "     *.${WALLY_TUNNEL_DOMAIN}  →  A record  →  $(curl -fsSL ifconfig.me 2>/dev/null || echo '<your-vps-ip>')"
echo ""
echo "  2. Install the client on your laptop:"
echo "     curl -fsSL https://raw.githubusercontent.com/wgawan/wally-tunnel/master/install.sh | bash"
echo ""
echo "  3. Create ~/.wally-tunnel.yaml:"
echo ""
echo "     server: tunnel.${WALLY_TUNNEL_DOMAIN}"
echo "     token: ${TOKEN}"
echo "     domain: ${WALLY_TUNNEL_DOMAIN}"
echo "     mappings:"
echo "       app: 3000"
echo ""
echo "  4. Run: wally-tunnel"
echo ""
