#!/usr/bin/env bash
# Installs unraid-shell-mcp as a systemd service on a generic Linux host
# (i.e. not the Unraid .plg path — see plugin/unraid-shell-mcp.plg for that).
# Also installs a second systemd service, unraid-shell-mcp-cloudflared, that
# reads tunnelMode from config.json and starts/skips a Cloudflare tunnel
# accordingly (same behavior as the Unraid plugin's rc.d cloudflared script).
#
# On a fresh install (no existing config.json) this interactively prompts,
# one at a time, for every setting config.json holds: listen address, bearer
# token, command whitelist/blacklist entries, the allowAllCommands opt-in,
# and Cloudflare tunnel mode/credentials — so the service can come up fully
# configured on first start instead of needing a manual edit afterward.
# Press Enter to accept the default shown in [brackets] for any prompt. If
# config.json already exists (e.g. re-running this script), it is left
# untouched and none of this is asked again.
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
#   NONINTERACTIVE     set to 1 to skip all prompts and use fail-closed
#                      defaults (empty whitelist, tunnelMode off), e.g. for
#                      scripted/unattended installs. Prompts are also
#                      skipped automatically when no controlling terminal
#                      (/dev/tty) is available.

set -euo pipefail

REPO="10bn/unraid-shell-mcp"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${CONFIG_DIR:-/etc/unraid-shell-mcp}"
BASE_URL="${BASE_URL:-https://github.com/${REPO}/releases}"
BINARY_NAME="unraid-shell-mcp"
CONFIG_FILE="${CONFIG_DIR}/config.json"
SERVICE_FILE="/etc/systemd/system/unraid-shell-mcp.service"
CLOUDFLARED_BIN="${INSTALL_DIR}/cloudflared"
CLOUDFLARED_WRAPPER="${INSTALL_DIR}/unraid-shell-mcp-cloudflared"
CLOUDFLARED_SERVICE_FILE="/etc/systemd/system/unraid-shell-mcp-cloudflared.service"

INTERACTIVE=1
if [ "${NONINTERACTIVE:-0}" = "1" ] || [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
    INTERACTIVE=0
fi

# ask PROMPT DEFAULT: prints the answer (or DEFAULT if blank/non-interactive)
# to stdout. Reads from /dev/tty, not the script's own stdin, since this
# script is normally run as `curl ... | sudo bash` — stdin is the script
# source itself, not a terminal.
ask() {
    local prompt="$1" default="$2" answer=""
    if [ "$INTERACTIVE" -eq 1 ]; then
        read -r -p "$prompt [$default]: " answer < /dev/tty || true
    fi
    printf '%s' "${answer:-$default}"
}

# confirm PROMPT: returns success (0) only if the user interactively types
# "yes"; always returns failure (1) when non-interactive or on any other
# input, so the caller's default is always the safe one.
confirm() {
    local prompt="$1" answer=""
    [ "$INTERACTIVE" -eq 1 ] || return 1
    read -r -p "$prompt Type 'yes' to confirm [no]: " answer < /dev/tty || true
    [ "$answer" = "yes" ]
}

# json_escape STRING: minimal JSON string escaping (backslash, double-quote)
# for the regex patterns and tokens this script embeds into config.json.
json_escape() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\"/\\\"}"
    printf '%s' "$s"
}

# json_array [STRING...]: prints a JSON array of string elements.
json_array() {
    local first=1 item
    printf '['
    for item in "$@"; do
        [ "$first" -eq 1 ] && first=0 || printf ', '
        printf '"%s"' "$(json_escape "$item")"
    done
    printf ']'
}

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

if [ -f "$CONFIG_FILE" ]; then
    echo "Existing config found at ${CONFIG_FILE}; leaving it untouched."
else
    LISTEN_ADDR="127.0.0.1:8483"
    BEARER_TOKEN=""
    WHITELIST=()
    BLACKLIST=()
    ALLOW_ALL="false"
    SETUP_TUNNEL_MODE="off"
    CF_TOKEN=""
    CF_HOSTNAME=""

    if [ "$INTERACTIVE" -eq 1 ]; then
        echo ""
        echo "--- unraid-shell-mcp first-time setup ---"
        echo "Press Enter to accept the default shown in [brackets] for any prompt."
        echo ""

        LISTEN_ADDR="$(ask "Listen address" "$LISTEN_ADDR")"

        BEARER_TOKEN="$(ask "Bearer token (blank = generate a random one, recommended)" "")"

        echo ""
        echo "Command whitelist: regex patterns the server may execute. A pattern must"
        echo "match the FULL command string, not just a prefix (e.g. '^echo\\b.*\$', not"
        echo "just '^echo\\b'). Enter one at a time; blank line to finish. An empty"
        echo "whitelist blocks all commands until you add one later."
        while true; do
            pattern="$(ask "  whitelist pattern (blank to finish)" "")"
            [ -z "$pattern" ] && break
            WHITELIST+=("$pattern")
        done

        echo ""
        echo "Command blacklist: regex patterns to always reject, checked before the"
        echo "whitelist (in addition to the hard-coded safety blocklist, which always"
        echo "applies and can't be configured). Blank line to finish; can be empty."
        while true; do
            pattern="$(ask "  blacklist pattern (blank to finish)" "")"
            [ -z "$pattern" ] && break
            BLACKLIST+=("$pattern")
        done

        echo ""
        if confirm "Allow ALL commands, bypassing the whitelist entirely (still subject to the hard-coded blocklist and any blacklist above)? Anyone with the bearer token gets full shell access."; then
            ALLOW_ALL="true"
        fi

        echo ""
        echo "Cloudflare tunnel mode: off (default), quick (no account, random URL),"
        echo "or named (stable hostname via Cloudflare Zero Trust)."
        while true; do
            SETUP_TUNNEL_MODE="$(ask "  tunnelMode (off/quick/named)" "off")"
            case "$SETUP_TUNNEL_MODE" in
                off|quick|named) break ;;
                *) echo "  Please enter off, quick, or named." ;;
            esac
        done
        if [ "$SETUP_TUNNEL_MODE" = "named" ]; then
            CF_TOKEN="$(ask "  cloudflareTunnelToken (from the Zero Trust dashboard)" "")"
            CF_HOSTNAME="$(ask "  cloudflareTunnelHostname" "")"
        fi
        echo ""
    else
        echo "No controlling terminal (or NONINTERACTIVE=1); skipping setup prompts."
        echo "Using fail-closed defaults (empty whitelist, tunnelMode off) — edit"
        echo "${CONFIG_FILE} afterward to configure."
    fi

    if [ -z "$BEARER_TOKEN" ]; then
        BEARER_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
    fi

    cat > "$CONFIG_FILE" <<EOF
{
  "bearerToken": "$(json_escape "$BEARER_TOKEN")",
  "listenAddr": "$(json_escape "$LISTEN_ADDR")",
  "commandWhitelist": $(json_array "${WHITELIST[@]}"),
  "commandBlacklist": $(json_array "${BLACKLIST[@]}"),
  "allowAllCommands": ${ALLOW_ALL},
  "tunnelMode": "$(json_escape "$SETUP_TUNNEL_MODE")",
  "cloudflareTunnelToken": "$(json_escape "$CF_TOKEN")",
  "cloudflareTunnelHostname": "$(json_escape "$CF_HOSTNAME")"
}
EOF
    chmod 0600 "$CONFIG_FILE"
    echo "Wrote ${CONFIG_FILE}."
fi

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

# Optional Cloudflare tunnel support, driven by tunnelMode in config.json
# (same "off"/"quick"/"named" values as the Unraid plugin). Installed
# unconditionally but harmless when tunnelMode is "off": the wrapper script
# just exits immediately in that case.
if [ ! -x "$CLOUDFLARED_BIN" ]; then
    echo "Downloading cloudflared..."
    if curl -fsSL -o "${CLOUDFLARED_BIN}.tmp" "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${ARCH}"; then
        chmod 0755 "${CLOUDFLARED_BIN}.tmp"
        mv "${CLOUDFLARED_BIN}.tmp" "$CLOUDFLARED_BIN"
    else
        echo "Warning: failed to download cloudflared; tunnelMode quick/named will" >&2
        echo "not work until ${CLOUDFLARED_BIN} exists and is executable." >&2
        rm -f "${CLOUDFLARED_BIN}.tmp"
    fi
fi

cat > "$CLOUDFLARED_WRAPPER" <<'EOF'
#!/bin/bash
# unraid-shell-mcp-cloudflared - optional Cloudflare tunnel for unraid-shell-mcp.
# Reads tunnel settings from config.json via `unraid-shell-mcp config-get`.
# See contrib/systemd/unraid-shell-mcp-cloudflared in the repo for the
# annotated version of this same script.

set -uo pipefail

BINARY="${UNRAID_SHELL_MCP_BINARY:-/usr/local/bin/unraid-shell-mcp}"
CONFIG_FILE="${UNRAID_SHELL_MCP_CONFIG:-/etc/unraid-shell-mcp/config.json}"
CLOUDFLARED="${CLOUDFLARED_BINARY:-/usr/local/bin/cloudflared}"
LOGFILE="/var/log/unraid-shell-mcp-cloudflared.log"
URLFILE="/run/unraid-shell-mcp-cloudflared-url"

config_get() {
    "$BINARY" config-get -config "$CONFIG_FILE" "$1" 2>/dev/null
}

watch_quick_url() {
    local deadline=$((SECONDS + 60))
    while [ "$SECONDS" -lt "$deadline" ]; do
        local url
        url="$(grep -oE 'https://[a-zA-Z0-9.-]+\.trycloudflare\.com' "$LOGFILE" 2>/dev/null | head -n1)"
        if [ -n "$url" ]; then
            echo "$url" >"$URLFILE"
            echo "Public URL: $url"
            return 0
        fi
        sleep 1
    done
}

tunnel_mode="$(config_get tunnelMode)"

if [ "$tunnel_mode" = "off" ] || [ -z "$tunnel_mode" ]; then
    echo "tunnelMode is off; nothing to do"
    exit 0
fi

if [ ! -x "$CLOUDFLARED" ]; then
    echo "cloudflared binary not found at $CLOUDFLARED" >&2
    exit 1
fi

rm -f "$URLFILE"
: >"$LOGFILE"

case "$tunnel_mode" in
    quick)
        listen_addr="$(config_get listenAddr)"
        echo "Starting cloudflared quick tunnel for http://${listen_addr}..."
        watch_quick_url &
        "$CLOUDFLARED" tunnel --no-autoupdate --url "http://${listen_addr}" 2>&1 | tee -a "$LOGFILE"
        ;;
    named)
        token="$(config_get cloudflareTunnelToken)"
        if [ -z "$token" ]; then
            echo "tunnelMode is 'named' but no cloudflareTunnelToken is configured" >&2
            exit 1
        fi
        echo "Starting cloudflared named tunnel..."
        "$CLOUDFLARED" tunnel --no-autoupdate run --token "$token" 2>&1 | tee -a "$LOGFILE"
        ;;
    *)
        echo "unknown tunnelMode: $tunnel_mode" >&2
        exit 1
        ;;
esac
EOF
chmod 0755 "$CLOUDFLARED_WRAPPER"

cat > "$CLOUDFLARED_SERVICE_FILE" <<EOF
[Unit]
Description=unraid-shell-mcp-cloudflared - optional Cloudflare tunnel for unraid-shell-mcp
After=network.target unraid-shell-mcp.service
Documentation=https://github.com/${REPO}

[Service]
Type=simple
Environment=UNRAID_SHELL_MCP_BINARY=${INSTALL_DIR}/${BINARY_NAME}
Environment=UNRAID_SHELL_MCP_CONFIG=${CONFIG_FILE}
Environment=CLOUDFLARED_BINARY=${CLOUDFLARED_BIN}
ExecStart=${CLOUDFLARED_WRAPPER}
Restart=on-failure
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
EOF

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable --now unraid-shell-mcp
    systemctl enable --now unraid-shell-mcp-cloudflared
    # Give the service a moment to write its first-run config.json.
    for _ in $(seq 1 10); do
        [ -f "$CONFIG_FILE" ] && break
        sleep 0.5
    done
    STATUS="$(systemctl is-active unraid-shell-mcp 2>&1 || true)"
    CF_STATUS="$(systemctl is-active unraid-shell-mcp-cloudflared 2>&1 || true)"
else
    echo "systemd is not running (e.g. inside a container); services were not" >&2
    echo "started automatically. Run the server manually with:" >&2
    echo "  ${INSTALL_DIR}/${BINARY_NAME} -config ${CONFIG_FILE}" >&2
    STATUS="not started (no systemd)"
    CF_STATUS="not started (no systemd)"
fi

TOKEN=""
if [ -f "$CONFIG_FILE" ]; then
    TOKEN="$(sed -n 's/.*"bearerToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG_FILE")"
fi

TUNNEL_MODE=""
if [ -f "$CONFIG_FILE" ]; then
    TUNNEL_MODE="$("${INSTALL_DIR}/${BINARY_NAME}" config-get -config "$CONFIG_FILE" tunnelMode 2>/dev/null || true)"
fi

WHITELIST_EMPTY=1
if [ -f "$CONFIG_FILE" ] && ! grep -q '"commandWhitelist": \[\]' "$CONFIG_FILE"; then
    WHITELIST_EMPTY=0
fi

echo ""
echo "-----------------------------------------------------------"
echo " unraid-shell-mcp installed."
echo " Service status:      ${STATUS}"
echo " cloudflared status:  ${CF_STATUS} (tunnelMode: ${TUNNEL_MODE:-off})"
echo " Config file:         ${CONFIG_FILE}"
if [ -n "$TOKEN" ]; then
    echo " Bearer token:        ${TOKEN}"
fi
echo ""
if [ "$WHITELIST_EMPTY" -eq 1 ]; then
    echo " The command whitelist is empty, so no commands can run yet. Edit"
    echo " commandWhitelist/commandBlacklist in ${CONFIG_FILE} (regex patterns,"
    echo " one per array entry — each must match the full command string),"
    echo " then:"
    echo "   systemctl restart unraid-shell-mcp"
else
    echo " Command whitelist/blacklist were set during setup. To change them,"
    echo " edit ${CONFIG_FILE}, then:"
    echo "   systemctl restart unraid-shell-mcp"
fi
echo ""
echo " To expose this over Cloudflare instead of just localhost, set"
echo " tunnelMode to \"quick\" (no account needed) or \"named\" (stable"
echo " hostname, needs cloudflareTunnelToken from the Zero Trust dashboard)"
echo " in ${CONFIG_FILE}, then:"
echo "   systemctl restart unraid-shell-mcp-cloudflared"
echo "   journalctl -u unraid-shell-mcp-cloudflared -f"
echo " In quick mode the ephemeral trycloudflare.com URL also gets written"
echo " to /run/unraid-shell-mcp-cloudflared-url."
echo ""
echo " WARNING: this exposes shell access to this machine to anyone holding"
echo " the bearer token above. See the README's Security section before"
echo " exposing it beyond localhost, and put Cloudflare Access in front of"
echo " any tunnel you enable."
echo "-----------------------------------------------------------"
echo ""
