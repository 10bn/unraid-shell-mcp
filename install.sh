#!/usr/bin/env bash
# Installs unraid-shell-mcp as a systemd service on a generic Linux host
# (i.e. not the Unraid .plg path — see plugin/unraid-shell-mcp.plg for that).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/install.sh | sudo bash
#
# Environment overrides:
#   VERSION            release tag to install, e.g. "v0.2.0" (default: latest)
#   INSTALL_DIR        where to put the binary (default: /usr/local/bin)
#   CONFIG_DIR         where config.json lives (default: /etc/unraid-shell-mcp)
#   BASE_URL           override the release base URL, mainly for testing
#                      against a local mirror (default: the GitHub repo's
#                      releases)

set -euo pipefail

REPO="10bn/unraid-shell-mcp"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${CONFIG_DIR:-/etc/unraid-shell-mcp}"
BASE_URL="${BASE_URL:-https://github.com/${REPO}/releases}"
BINARY_NAME="unraid-shell-mcp"
CONFIG_FILE="${CONFIG_DIR}/config.json"
SERVICE_FILE="/etc/systemd/system/unraid-shell-mcp.service"

if [ "$(id -u)" -ne 0 ]; then
    echo "This installer needs to write to ${INSTALL_DIR}, ${CONFIG_DIR}, and" >&2
    echo "/etc/systemd/system; re-run it as root (e.g. with sudo)." >&2
    exit 1
fi

case "$(uname -s)" in
    Linux) ;;
    *)
        echo "unraid-shell-mcp only ships Linux binaries; unsupported OS: $(uname -s)" >&2
        exit 1
        ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *)
        echo "Unsupported architecture: $(uname -m) (only amd64 and arm64 are built)" >&2
        exit 1
        ;;
esac

ASSET="unraid-shell-mcp-linux-${ARCH}.tar.gz"
if [ "$VERSION" = "latest" ]; then
    ASSET_URL="${BASE_URL}/latest/download/${ASSET}"
else
    ASSET_URL="${BASE_URL}/download/${VERSION}/${ASSET}"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${ASSET} from ${ASSET_URL}..."
curl -fsSL -o "${TMPDIR}/${ASSET}" "$ASSET_URL"
tar -xzf "${TMPDIR}/${ASSET}" -C "$TMPDIR"

if [ ! -x "${TMPDIR}/${BINARY_NAME}" ]; then
    echo "Downloaded archive did not contain an executable named ${BINARY_NAME}" >&2
    exit 1
fi

echo "Installing binary to ${INSTALL_DIR}/${BINARY_NAME}..."
install -d "$INSTALL_DIR"
install -m 0755 "${TMPDIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

echo "Preparing config directory ${CONFIG_DIR}..."
install -d -m 0700 "$CONFIG_DIR"

echo "Installing systemd unit at ${SERVICE_FILE}..."
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=unraid-shell-mcp - MCP server for whitelisted shell command execution
After=network.target
Documentation=https://github.com/${REPO}

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME} -config ${CONFIG_FILE}
Restart=on-failure
RestartSec=2
User=root

[Install]
WantedBy=multi-user.target
EOF

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable --now unraid-shell-mcp
    # Give the service a moment to write its first-run config.json.
    for _ in $(seq 1 10); do
        [ -f "$CONFIG_FILE" ] && break
        sleep 0.5
    done
    STATUS="$(systemctl is-active unraid-shell-mcp 2>&1 || true)"
else
    echo "systemd is not running (e.g. inside a container); service was not" >&2
    echo "started automatically. Run it manually with:" >&2
    echo "  ${INSTALL_DIR}/${BINARY_NAME} -config ${CONFIG_FILE}" >&2
    STATUS="not started (no systemd)"
fi

TOKEN=""
if [ -f "$CONFIG_FILE" ]; then
    TOKEN="$(sed -n 's/.*"bearerToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG_FILE")"
fi

echo ""
echo "-----------------------------------------------------------"
echo " unraid-shell-mcp installed."
echo " Service status: ${STATUS}"
echo " Config file:    ${CONFIG_FILE}"
if [ -n "$TOKEN" ]; then
    echo " Bearer token:   ${TOKEN}"
fi
echo ""
echo " The command whitelist is empty by default, so no commands can run"
echo " yet. Edit commandWhitelist/commandBlacklist in ${CONFIG_FILE} (regex"
echo " patterns, one per array entry), then:"
echo "   systemctl restart unraid-shell-mcp"
echo ""
echo " WARNING: this exposes shell access to this machine to anyone holding"
echo " the bearer token above. See the README's Security section before"
echo " exposing it beyond localhost."
echo "-----------------------------------------------------------"
echo ""
