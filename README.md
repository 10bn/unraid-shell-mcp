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
   hostname) with header `Authorization: Bearer <token>`. If your client has
   no way to set a custom header, use `.../mcp/<token>` instead (same
   token, embedded in the URL) and select "no authentication" — the
   Settings page shows this exact URL ready to copy. See "Two ways to
   authenticate" below.

## Install on generic Linux (systemd)

Not on Unraid? `install.sh` installs the same binary as a systemd service on
any `amd64`/`arm64` Linux host:

```sh
curl -fsSL https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/install.sh | sudo bash
```

This downloads the latest release's binary for your architecture and installs
it to `/usr/local/bin/unraid-shell-mcp`. On a fresh install (no config.json
yet) it then walks you through **every setting config.json holds**, one
prompt at a time — press Enter to accept the default shown in `[brackets]`:

- listen address
- bearer token (blank to auto-generate a random one — recommended; there is
  no default token either way)
- command whitelist entries (blank line to stop adding more; leaving it
  empty means no commands can run until you add one later)
- command blacklist entries (same, optional)
- whether to enable `allowAllCommands` (asks you to type `yes` to confirm —
  see the Security section on what this means)
- Cloudflare tunnel mode (`off`/`quick`/`named`) and, for `named`, the
  Cloudflare tunnel token/hostname

It then writes `/etc/unraid-shell-mcp/config.json` (`0600`) with your
answers, writes a `unraid-shell-mcp.service` unit to `/etc/systemd/system/`,
installs `cloudflared` and a second service, `unraid-shell-mcp-cloudflared`,
that acts on whatever `tunnelMode` you chose (harmless and idle if you left
it `off`, the default — see "Cloudflare tunnel" below), and starts both.
The bearer token is printed to your terminal at the end — copy it now, since
nothing else displays it again (there's no webGUI outside Unraid).

Running it non-interactively (no terminal attached, e.g. from another
script) or with `NONINTERACTIVE=1` skips all of the above and falls back to
the same fail-closed defaults as before: empty whitelist, `tunnelMode off`,
random token. **Re-running the installer never touches an existing
config.json** — if one is already there, none of this is asked again and
your settings are left exactly as they are; edit the file directly and
restart the relevant service instead:

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

Install a specific version instead of latest, override install paths, or
force a non-interactive install with defaults:

```sh
VERSION=v0.2.0 INSTALL_DIR=/opt/bin CONFIG_DIR=/opt/etc/unraid-shell-mcp NONINTERACTIVE=1 \
  curl -fsSL https://raw.githubusercontent.com/10bn/unraid-shell-mcp/main/install.sh | sudo -E bash
```

This path is fully independent of the Unraid plugin — no rc.d, no `.txz`, no
webGUI — but it is functionally equivalent for the Cloudflare tunnel piece.

### Cloudflare tunnel (generic Linux)

By default `tunnelMode` is `"off"` and `unraid-shell-mcp-cloudflared` idles.
To expose the server, edit `/etc/unraid-shell-mcp/config.json`:

```sh
sudo systemctl stop unraid-shell-mcp-cloudflared   # not required, but avoids a
                                                    # half-started tunnel while editing
sudo nano /etc/unraid-shell-mcp/config.json         # set tunnelMode to "quick" or "named"
sudo systemctl restart unraid-shell-mcp-cloudflared
sudo journalctl -u unraid-shell-mcp-cloudflared -f  # watch it come up
```

- `"quick"`: no Cloudflare account needed; an ephemeral `*.trycloudflare.com`
  URL is assigned each time the service starts. It's printed in the journal
  and also written to `/run/unraid-shell-mcp-cloudflared-url`.
- `"named"`: set `cloudflareTunnelToken` (from the Cloudflare Zero Trust
  dashboard) and `cloudflareTunnelHostname` in config.json first — this gets
  you a stable hostname instead of a random one.

## Two ways to authenticate

Every request needs the bearer token one way or another; there's no
"unauthenticated" mode. Which of these two you use is just a matter of what
your MCP client's UI can configure:

- **Header (preferred when your client supports it):** point the client at
  `.../mcp` and set `Authorization: Bearer <token>`.
- **URL-embedded token:** point the client at `.../mcp/<token>` instead and
  select "no authentication" (or leave auth unconfigured) — the token
  travels in the path rather than a header, for clients whose UI only
  accepts a URL with no way to add a custom header. It's the exact same
  secret either way, just carried differently; the Settings page (Unraid)
  shows this full URL pre-composed with the tunnel hostname and token
  already filled in, ready to paste.

The tradeoff: URLs are more likely than headers to end up somewhere you
didn't intend — browser history, a reverse proxy's access log, a screenshot.
Prefer the header form when you can; use the URL form when you can't.

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
  `false`. Understand that enabling it hands full shell access to anyone
  with the bearer token before you flip it.
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
- Two ways to authenticate, both checking the same token, either works: a
  `net/http` middleware on `/mcp` requires `Authorization: Bearer <token>`;
  a second one on `/mcp/<token>` reads the token from the URL path itself,
  for MCP clients whose UI only accepts a URL with no way to set a custom
  header. Pick whichever your client supports — see "Two ways to
  authenticate" below for the tradeoff.
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
  publicly. On both install paths, a small wrapper reads tunnel settings from
  the same config file via `unraid-shell-mcp config-get <field>` (no JSON
  tooling needed in the shell script) and, in quick mode, parses the
  ephemeral `*.trycloudflare.com` URL out of the cloudflared log — on Unraid
  that's `rc.unraid-shell-mcp-cloudflared`, writing the URL to
  `/var/run/unraid-shell-mcp-cloudflared-url.txt` for the Settings page; on
  generic Linux it's the `unraid-shell-mcp-cloudflared` systemd service
  install.sh sets up, writing the URL to
  `/run/unraid-shell-mcp-cloudflared-url` and to its own journal.
- On generic Linux, `install.sh` installs the binary + a systemd unit pointed
  at `/etc/unraid-shell-mcp/config.json`, plus the cloudflared tunnel service
  above; there's no rc.d, no `.txz`, no webGUI — edit the config file by hand
  and `systemctl restart` the relevant service to pick up changes.

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
contrib/systemd/          reference copies of the systemd units/scripts install.sh generates
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
- **cloudflared itself has not been exercised end-to-end by this project's own
  development environment** (its sandboxed network policy blocks fetching the
  `cloudflared` binary directly). The quick/named-mode logic in both
  `rc.unraid-shell-mcp-cloudflared` (Unraid) and `unraid-shell-mcp-cloudflared`
  (generic Linux) — mode switching, URL-file writing, clean process teardown,
  no leaked child processes — was verified against a fake `cloudflared`
  binary that reproduces the real log format. It has since been confirmed
  installing correctly (binary downloaded, service enabled) on a real Debian
  host outside this sandbox; running an actual tunnel end-to-end there is
  still unverified.
- **`install.sh` has been run for real** against the project's own published
  GitHub release (not just a mocked one): downloading the actual release
  tarball, installing the binary, generating `config.json` with correct
  `0600` permissions, and confirming bearer-token auth against the installed
  binary. It has also been run successfully on a real systemd host (Debian)
  outside this sandbox, including the `systemctl enable --now` path this
  sandbox itself can't exercise (no running systemd instance here). The
  interactive first-run prompts (listen address, whitelist/blacklist entries,
  `allowAllCommands`, tunnel mode, JSON escaping of special characters in
  patterns) were exercised end-to-end against a real pseudo-terminal in this
  sandbox — necessary because these prompts read from `/dev/tty`, not stdin,
  so they can't be driven by ordinary piped input — confirming the generated
  `config.json` matched the answers exactly and that the installed binary
  enforced them correctly (auth, whitelist/blacklist, `allowAllCommands`) on
  first start.
- No Unraid Community Applications submission — install via the raw `.plg`
  URL above only.
