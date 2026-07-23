# unraid-shell-mcp

A native Unraid plugin that runs a [Model Context Protocol](https://modelcontextprotocol.io)
(MCP) server directly on your Unraid NAS, exposing a single `execute-command`
tool that runs shell commands on the host — gated by a bearer token and a
fail-closed command whitelist.

Unlike an ssh-mcp-server-style setup (an MCP server on a control host that
SSHes into the target), there is no SSH hop, no control host, no keys to
manage: the MCP server *is* the Unraid box.
That also means it is the **only** line of defense between an MCP client (or
anyone with the bearer token) and full root access to your array, Docker
containers, VMs, and shares. Read the "Security" section below before
installing this.

## Install

1. On your Unraid server, go to **Plugins → Install Plugin**.
2. Paste this URL and click **Install**:

   ```
   https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/plugin/unraid-shell-mcp.plg
   ```

3. On first install, a random bearer token is generated and a config file is
   created at `/boot/config/plugins/unraid-shell-mcp/config.json` with an
   **empty command whitelist** — no commands can run until you configure one.
4. Go to **Settings → Unraid Shell MCP** to view/rotate the bearer token,
   configure the command whitelist/blacklist, and (optionally) enable a
   Cloudflare tunnel for remote access.
5. Point your MCP client at `http://<unraid-ip>:8483/mcp` (or your tunnel
   hostname) with header `Authorization: Bearer <token>`.

## Security

**This plugin gives whoever holds the bearer token full shell access to your
NAS** — the array, Docker, VMs, every share, everything. Treat the token like
a root password, because it effectively is one.

- **Fail closed by default.** With an empty `commandWhitelist`, every
  `execute-command` call is rejected. There is no "empty whitelist = allow
  everything" fallback.
- **Whitelist first, then blacklist, then a hard-coded, non-configurable
  blocklist for catastrophic operations** (raw writes to `/dev/sd*`/`/dev/md*`,
  destructive `mdcmd` array commands, `mkfs`/`wipefs`/`shred` against block
  devices, `rm -rf /`, fork bombs, etc.) is checked on every command — in that
  order — and the hard blocklist cannot be overridden by your whitelist, even
  a maximally permissive one.
- **The bearer token is generated randomly on first install.** There is no
  default token. Rotate it any time from the Settings page.
- **`config.json` is written with `0600` permissions** and is never served by
  the webGUI without going through Unraid's own authenticated session.
- **Keep the whitelist as narrow as possible.** Prefer specific, anchored
  patterns (`^docker ps$`) over broad ones (`^docker`).
- **If you expose this outside your LAN (e.g. via the built-in Cloudflare
  tunnel), put it behind
  [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)**
  so requests need an authenticated identity in addition to the bearer token.
  The tunnel alone only gets you a routable hostname, not authorization.

If any of that is more risk than you want to take on, don't install this
plugin — a curated, capability-scoped API (not free shell access) would be
the safer choice, and is intentionally not what this project provides.

## How it works

- A single statically linked Go binary (`unraid-shell-mcp`) runs an MCP
  server over Streamable HTTP at `/mcp`, using
  [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).
- A `net/http` middleware checks `Authorization: Bearer <token>` on every
  request before it reaches the MCP handler.
- The one MCP tool, `execute-command`, runs `/bin/sh -c <command>` after
  checking the command against the hard blocklist, then the configured
  blacklist, then the configured whitelist (see `internal/whitelist`).
- Configuration lives at `/boot/config/plugins/unraid-shell-mcp/config.json`
  (persists across reboots, since Unraid boots from a RAM overlay and only
  `/boot` is durable). See `config.example.json` for the shape.
- An optional `cloudflared` tunnel (quick or named mode) exposes the server
  publicly; its rc.d script reads tunnel settings from the same config file
  via `unraid-shell-mcp config-get <field>` (no JSON tooling needed in the
  shell script) and, in quick mode, parses the ephemeral `*.trycloudflare.com`
  URL out of the cloudflared log and writes it to
  `/var/run/unraid-shell-mcp-cloudflared-url.txt` for the Settings page to
  display.

## Repository layout

```
cmd/unraid-shell-mcp/     main package: CLI flags, HTTP server wiring
internal/mcp/             the execute-command MCP tool
internal/auth/            bearer-token HTTP middleware
internal/config/          config.json load/save
internal/whitelist/       whitelist/blacklist + hard-coded blocklist matcher
plugin/
  unraid-shell-mcp.plg    Unraid plugin descriptor (install/remove logic)
  package/                Slackware package skeleton + Makefile (makepkg)
  rc.d/                   rc.unraid-shell-mcp, rc.unraid-shell-mcp-cloudflared
webgui/UnraidShellMcp.page  Settings page (token, whitelist, tunnel, status)
.github/workflows/release.yml  builds + publishes .txz/.plg on tag push
config.example.json
```

## Building from source

Requires Go 1.25+.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/unraid-shell-mcp ./cmd/unraid-shell-mcp
go test ./...
```

To build the Slackware `.txz` package used by the plugin installer:

```sh
make -C plugin/package VERSION=0.1.0
```

This uses the real `makepkg` (part of Slackware's `pkgtools`) when available.
On non-Slackware build hosts — including the GitHub Actions runner this
project's release workflow uses — `makepkg` isn't installed, so the Makefile
falls back to assembling the same file layout with `tar`+`xz` directly. See
"Known limitations" below.

Running locally without installing the plugin:

```sh
go run ./cmd/unraid-shell-mcp -config ./config.example.json -listen 127.0.0.1:8483
```

## Known limitations

- **`.txz` packaging has not been verified on a real Slackware/Unraid host
  or `makepkg`.** No Unraid host or Slackware environment was available to
  test against; the fallback tar+xz packaging was checked by hand-extracting
  the resulting archive and confirming the expected file tree, permissions,
  and a statically linked binary, and by comparing the `.plg` structure
  against a real published plugin
  ([NerdPack.plg](https://raw.githubusercontent.com/dmacias72/unRAID-plugins/master/plugins/NerdPack.plg)).
  It has not been installed on an actual Unraid boot device.
- **cloudflared itself was not exercised end-to-end** (this sandbox's network
  policy blocks fetching the `cloudflared` binary). The quick-tunnel URL
  parsing logic in `rc.unraid-shell-mcp-cloudflared` was tested against a
  fake `cloudflared` binary that reproduces the real log format.
- No Unraid Community Applications submission — install via the raw `.plg`
  URL above only.
