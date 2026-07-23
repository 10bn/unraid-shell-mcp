# unraid-shell-mcp

Runs a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server
directly on a host, exposing a single `execute-command` tool that runs shell
commands on that host — gated by a bearer token and a fail-closed command
whitelist. Ships two ways: as a native Unraid plugin, and as a plain
systemd service for any other Linux box.

Unlike an ssh-mcp-server-style setup (an MCP server on a control host that
SSHes into the target), there is no SSH hop, no control host, no keys to
manage: the MCP server *is* the target box.
That also means it is the **only** line of defense between an MCP client (or
anyone with the bearer token) and full root access to that machine — the
array, Docker, VMs, and shares on Unraid, or just the whole filesystem on a
generic Linux box. Read the "Security" section below before installing this.

## Install on Unraid

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

## Install on generic Linux (systemd)

Not on Unraid? `install.sh` installs the same binary as a systemd service on
any `amd64`/`arm64` Linux host:

```sh
curl -fsSL https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/install.sh | sudo bash
```

This downloads the latest release's binary for your architecture, installs it
to `/usr/local/bin/unraid-shell-mcp`, writes a `unraid-shell-mcp.service` unit
to `/etc/systemd/system/`, and starts it. On first start it generates
`/etc/unraid-shell-mcp/config.json` (`0600`, random bearer token, empty
whitelist) and prints the token to your terminal — copy it now, since nothing
else displays it again (there's no webGUI outside Unraid).

The whitelist is empty by default, so no commands can run yet. Edit
`commandWhitelist`/`commandBlacklist` in that config file (one regex per
array entry — see `config.example.json`), then:

```sh
sudo systemctl restart unraid-shell-mcp
```

Useful commands:

```sh
sudo systemctl status unraid-shell-mcp   # check it's running
sudo journalctl -u unraid-shell-mcp -f   # follow logs
sudo ./uninstall.sh                      # remove binary + service (keeps config)
sudo ./uninstall.sh --purge              # also delete the config (token, whitelist)
```

Install a specific version instead of latest, or override install paths:

```sh
VERSION=v0.2.0 INSTALL_DIR=/opt/bin CONFIG_DIR=/opt/etc/unraid-shell-mcp \
  curl -fsSL https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/install.sh | sudo -E bash
```

This path is fully independent of the Unraid plugin — no rc.d, no `.txz`, no
cloudflared integration (run your own tunnel/reverse proxy in front of it if
you need remote access, and put an authenticating proxy like
Cloudflare Access in front of that).

## Security

**Whoever holds the bearer token gets full shell access to this machine** —
on Unraid that's the array, Docker, VMs, every share; on generic Linux it's
whatever that user account (this runs as `root` by default) can reach. Treat
the token like a root password, because it effectively is one.

- **Fail closed by default.** With an empty `commandWhitelist`, every
  `execute-command` call is rejected. There is no "empty whitelist = allow
  everything" fallback — the only way to allow everything is the explicit
  `allowAllCommands` opt-in described below, which defaults to `false` and
  has to be deliberately set.
- **Whitelist first, then blacklist, then a hard-coded, non-configurable
  blocklist for catastrophic operations** (raw writes to `/dev/sd*`/`/dev/md*`,
  destructive `mdcmd` array commands, `mkfs`/`wipefs`/`shred` against block
  devices, `rm -rf /`, fork bombs, etc.) is checked on every command — in that
  order — and the hard blocklist cannot be overridden by your whitelist, even
  a maximally permissive one.
- **The bearer token is generated randomly on first run.** There is no
  default token. Rotate it any time from the Settings page (Unraid) or by
  editing `bearerToken` in `config.json` and restarting the service
  (generic Linux).
- **`config.json` is written with `0600` permissions.** On Unraid it's never
  served by the webGUI without going through Unraid's own authenticated
  session; on generic Linux, nothing but the service itself and root reads it.
- **Whitelist patterns must match the entire command, not just a prefix.**
  Commands run via `/bin/sh -c <command>`, so a pattern like `^echo\b` under
  ordinary "appears somewhere in the string" regex matching would also
  match — and therefore fully execute — `echo hi; cat /etc/shadow`, since
  the pattern only checks that the string *starts* with `echo`. To close
  that off, whitelist patterns are required to match the command's entire
  length; that command would need `^echo\b.*$` to be allowed, and the
  operator, by writing `.*$`, is explicitly the one deciding to allow
  anything after `echo`. The blacklist and hard blocklist intentionally
  keep the opposite ("appears anywhere") semantics, so they still catch a
  dangerous fragment tucked after a `;` in an otherwise-permitted command.
- **`allowAllCommands` (default `false`) is an explicit opt-in that skips
  the whitelist requirement entirely** — every command becomes eligible to
  run, subject only to the hard blocklist and `commandBlacklist`, which
  still apply and cannot be bypassed by it. It exists for cases like local
  testing where maintaining a whitelist is more friction than it's worth;
  it is never the default, a fresh install/config file always has it
  `false`, and enabling it prints a large, hard-to-miss warning in the
  server log on every start. Toggling it in the webGUI requires an extra
  JS confirmation dialog on save. Understand that enabling it hands full
  shell access to anyone with the bearer token before you flip it.
- **Output is capped at 1 MiB per stream (stdout/stderr).** A command that
  exceeds it is terminated immediately rather than left running until its
  timeout while consuming unbounded memory (e.g. `yes`, or `cat` on a huge
  file).
- **Every invocation is audit-logged** as a structured JSON line on stdout —
  the command text, outcome (`success`/`rejected`/`timeout`/`output_cap`/
  `error`), exit code, and duration — captured by the rc.d log file on
  Unraid and by `journalctl` under systemd, with no extra configuration.
- **If you expose this outside your LAN** (e.g. via the Unraid plugin's
  built-in Cloudflare tunnel, or your own reverse proxy in front of the
  generic-Linux install), **put it behind
  [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)**
  or equivalent so requests need an authenticated identity in addition to the
  bearer token. A tunnel or reverse proxy alone only gets you a routable
  hostname, not authorization.

If any of that is more risk than you want to take on, don't install this —
a curated, capability-scoped API (not free shell access) would be the safer
choice, and is intentionally not what this project provides.

## How it works

- A single statically linked Go binary (`unraid-shell-mcp`) runs an MCP
  server over Streamable HTTP at `/mcp`, using
  [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).
- A `net/http` middleware checks `Authorization: Bearer <token>` on every
  request before it reaches the MCP handler.
- The one MCP tool, `execute-command`, runs `/bin/sh -c <command>` after
  checking the command against the hard blocklist, then the configured
  blacklist (both substring/"appears anywhere" matches), then the configured
  whitelist, which must match the command's *entire* length (see
  `internal/whitelist`). Output is capped at 1 MiB per stream — a command
  that runs past that is killed (as a process group, so backgrounded or
  piped children die too, not just the immediate `/bin/sh` process) rather
  than left running until its timeout. Every invocation, allowed or
  rejected, is logged as a structured JSON audit line on stdout.
- Configuration lives at `/boot/config/plugins/unraid-shell-mcp/config.json`
  (persists across reboots, since Unraid boots from a RAM overlay and only
  `/boot` is durable). See `config.example.json` for the shape.
- An optional `cloudflared` tunnel (quick or named mode) exposes the server
  publicly; its rc.d script reads tunnel settings from the same config file
  via `unraid-shell-mcp config-get <field>` (no JSON tooling needed in the
  shell script) and, in quick mode, parses the ephemeral `*.trycloudflare.com`
  URL out of the cloudflared log and writes it to
  `/var/run/unraid-shell-mcp-cloudflared-url.txt` for the Settings page to
  display. This tunnel integration is Unraid-only; `install.sh` does not set
  up cloudflared.
- On generic Linux, `install.sh` just installs the binary + a systemd unit
  pointed at `/etc/unraid-shell-mcp/config.json`; there's no rc.d, no `.txz`,
  no webGUI — edit the config file by hand and `systemctl restart` to pick up
  changes.

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
install.sh                generic-Linux systemd installer
uninstall.sh              removes the install.sh install
contrib/systemd/          reference copy of the systemd unit install.sh generates
.github/workflows/release.yml  builds + publishes .txz/.plg/tarballs on tag push
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
- **`install.sh` was tested against a local mock release server** (via its
  `BASE_URL` override), covering the download/install/config-generation path
  end-to-end, including running the installed binary to confirm the
  generated `config.json`, its `0600` permissions, and bearer-token auth all
  work. `systemctl enable --now` itself was not exercised (this sandbox has
  no running systemd instance); the script degrades to printing a manual-run
  command when systemd isn't available, which was exercised, but the actual
  `systemctl enable`/service-management path on a real systemd host has not
  been.
- No Unraid Community Applications submission — install via the raw `.plg`
  URL above only.
