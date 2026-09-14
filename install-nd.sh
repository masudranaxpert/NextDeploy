#!/bin/sh
# install-nd.sh — Install NextDeploy CLI (nd) on Linux and macOS
set -e

REPO="masudranaxpert/NextDeploy"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux*)  OS="linux" ;;
    darwin*) OS="darwin" ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

BINARY="nd-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"

echo "→ Installing nd CLI (${OS}/${ARCH})..."
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "$TMP/nd"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMP/nd" "$URL"
else
    echo "Error: curl or wget is required to install nd"
    exit 1
fi

chmod +x "$TMP/nd"

# Check write access to /usr/local/bin
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP/nd" "$INSTALL_DIR/nd"
else
    if command -v sudo >/dev/null 2>&1; then
        echo "Elevating permissions with sudo to write to $INSTALL_DIR..."
        sudo mv "$TMP/nd" "$INSTALL_DIR/nd"
    else
        USER_BIN="$HOME/.local/bin"
        mkdir -p "$USER_BIN"
        mv "$TMP/nd" "$USER_BIN/nd"
        INSTALL_DIR="$USER_BIN"
        echo "Note: installed to $USER_BIN. Make sure it is in your PATH."
    fi
fi

echo "✓ nd CLI installed successfully to $INSTALL_DIR/nd"
echo "  Run 'nd --version' or 'nd login <server_url>' to get started."