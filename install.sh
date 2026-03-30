#!/usr/bin/env bash
set -euo pipefail

# Install the wally-tunnel client from the latest GitHub release.
# Usage: curl -fsSL https://raw.githubusercontent.com/wgawan/wally-tunnel/master/install.sh | bash

REPO="wgawan/wally-tunnel"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)
        echo "Error: unsupported OS: $OS"
        exit 1
        ;;
esac

BINARY_NAME="wally-tunnel-${OS}-${GOARCH}"

echo "Downloading latest wally-tunnel (${OS}/${GOARCH})..."
DOWNLOAD_URL=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep "browser_download_url.*${BINARY_NAME}\"" \
    | head -1 \
    | cut -d '"' -f 4)

if [ -z "$DOWNLOAD_URL" ]; then
    echo "Error: could not find release binary for ${BINARY_NAME}"
    echo "You can build from source instead: go install github.com/${REPO}/cmd/wally-tunnel@latest"
    exit 1
fi

TMP=$(mktemp)
curl -fsSL -o "$TMP" "$DOWNLOAD_URL"
chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP" "${INSTALL_DIR}/wally-tunnel"
else
    sudo mv "$TMP" "${INSTALL_DIR}/wally-tunnel"
fi

echo "Installed wally-tunnel to ${INSTALL_DIR}/wally-tunnel"
echo ""
echo "Create ~/.wally-tunnel.yaml with your server details, then run: wally-tunnel"
