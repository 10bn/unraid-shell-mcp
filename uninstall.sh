#!/usr/bin/env bash
# Removes the generic-Linux systemd install created by install.sh.
# Does NOT touch the Unraid .plg install (see plugin/unraid-shell-mcp.plg).
#
# Usage: sudo ./uninstall.sh [--purge]
#   --purge   also delete the config directory (bearer token, whitelist)

set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${CONFIG_DIR:-/etc/unraid-shell-mcp}"
BINARY_NAME="unraid-shell-mcp"
SERVICE_FILE="/etc/systemd/system/unraid-shell-mcp.service"
CLOUDFLARED_SERVICE_FILE="/etc/systemd/system/unraid-shell-mcp-cloudflared.service"
PURGE=0

for arg in "$@"; do
    case "$arg" in
        --purge) PURGE=1 ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "Re-run as root (e.g. with sudo)." >&2
    exit 1
fi

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl disable --now unraid-shell-mcp 2>/dev/null || true
    systemctl disable --now unraid-shell-mcp-cloudflared 2>/dev/null || true
fi

rm -f "$SERVICE_FILE" "$CLOUDFLARED_SERVICE_FILE"
command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] && systemctl daemon-reload || true

rm -f "${INSTALL_DIR}/${BINARY_NAME}" \
      "${INSTALL_DIR}/unraid-shell-mcp-cloudflared" \
      "${INSTALL_DIR}/cloudflared" \
      /var/log/unraid-shell-mcp-cloudflared.log \
      /run/unraid-shell-mcp-cloudflared-url

if [ "$PURGE" -eq 1 ]; then
    rm -rf "$CONFIG_DIR"
    echo "unraid-shell-mcp removed, including ${CONFIG_DIR}."
else
    echo "unraid-shell-mcp removed. Config left at ${CONFIG_DIR} (bearer token,"
    echo "whitelist). Re-run with --purge to delete it too."
fi
