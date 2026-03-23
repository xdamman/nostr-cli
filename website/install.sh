#!/bin/sh
set -e

REPO="xdamman/nostr-cli"
BINARY="nostr"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest release tag
LATEST=$(curl -sf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Error: could not determine latest release"
  exit 1
fi

TARBALL="nostr_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${TARBALL}"

echo "Downloading ${BINARY} ${LATEST} for ${OS}/${ARCH}..."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -sfL "$URL" -o "${TMP}/${TARBALL}"
tar -xzf "${TMP}/${TARBALL}" -C "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo "Installed ${BINARY} ${LATEST} to ${INSTALL_DIR}/${BINARY}"
