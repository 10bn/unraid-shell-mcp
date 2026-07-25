# unraid-shell-mcp

A native Unraid plugin that runs a [Model Context Protocol](https://modelcontextprotocol.io)
(MCP) server directly on the box, exposing a single `execute-command` tool
that runs shell commands on that host — gated by a bearer token and a
fail-closed command whitelist.

Unlike an ssh-mcp-server-style setup (an MCP server on a control host that
SSHes into the target), there is no SSH hop, no control host, no keys to
manage: the MCP server *is* the target box.
That also means it is the **only** line of defense between an MCP client (or
anyone with the bearer token) and full root access to that machine — the
array, Docker, VMs, and shares. Read the "Security" section below before
installing this.

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
5. Point your MCP client at the server with header
   `Authorization: Bearer <token>`. Note the server listens on
   `127.0.0.1:8483` by default — reachable from the Unraid box itself and
   through the tunnel, but *not* from other machines on your LAN. For
   remote clients, use one of the Cloudflare tunnel modes (recommended);
   for direct LAN access instead, change `listenAddr` to `0.0.0.0:8483` in
   `config.json` and restart the server, then use
   `http://<unraid-ip>:8483/mcp`. If your client has no way to set a
   custom header, use `.../mcp/<token>` instead (same token, embedded in
   the URL) and select "no authentication" — the Settings page shows this
   exact URL ready to copy. See "Two ways to authenticate" below.
6. Both the MCP server and (if configured) the tunnel start automatically
   when the array comes up after a reboot — no manual start needed. Each
   has its own "Start at boot" checkbox on the Settings page if you'd
   rather start one (or both) by hand.

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
  secret either way, just carried differently; the Settings page shows this
  full URL pre-composed with the tunnel hostname and token already filled
  in, ready to paste.

The tradeoff: URLs are more likely than headers to end up somewhere you
didn't intend — browser history, a reverse proxy's access log, a screenshot.
Prefer the header form when you can; use the URL form when you can't.

## Cloudflare tunnel modes

Three ways to expose the server beyond your LAN, configurable from the
Settings page's "Remote access" section:

- **Quick tunnel** — no Cloudflare account needed. Gets you a random
  `*.trycloudflare.com` hostname that changes every time the tunnel
  restarts. Good for a quick test, not for a client you'll want to
  reconnect to later.
- **Authorized tunnel** (`tunnelMode: "local"`) — a stable hostname of your
  choosing, set up without ever touching the Cloudflare dashboard. Enter
  the hostname, save, and start the tunnel; the Settings page then shows an
  **"Authorize with Cloudflare"** link. Click it (any device's browser
  works — it doesn't need to reach this NAS), approve it in your Cloudflare
  account, and the plugin runs `cloudflared tunnel create` + `tunnel route
  dns` itself and starts serving. This is what Cloudflare's own docs call a
  "locally-managed tunnel": its ingress configuration lives on this NAS
  (under `/boot/config/plugins/unraid-shell-mcp/cloudflared/`, so it
  survives reboots), not in Cloudflare's cloud.
- **Managed tunnel** (`tunnelMode: "named"`) — also a stable hostname, but
  you create the tunnel yourself first in the
  [Cloudflare Zero Trust dashboard](https://one.dash.cloudflare.com/) and
  paste its token into the Cloudflare tunnel token field. Cloudflare calls
  this a "remotely-managed tunnel": the ingress configuration lives in
  Cloudflare's cloud, pulled down via the token, so any change (new
  hostname, different backend) is made in the dashboard, not here.

Authorized mode is the more automated/convenient of the two stable-hostname
options; managed mode is the one to reach for if you'd rather keep tunnel
configuration centralized in Cloudflare's dashboard, or already manage other
tunnels that way. Either way, changing the hostname later doesn't delete the
old DNS record automatically — Cloudflare's own tooling doesn't do this
either — so clean up a stale one by hand in the dashboard if you no longer
want it.

## Security

**Whoever holds the bearer token gets full shell access to this machine** —
the array, Docker, VMs, every share. Treat the token like a root password,
because it effectively is one.

- **Fail closed by default.** With an empty `commandWhitelist`, every
  `execute-command` call is rejected. There is no "empty whitelist = allow
  everything" fallback — the only way to allow everything is the explicit
  `allowAllCommands` opt-in described below, which defaults to `false` and
  has to be deliberately set.
- **Whitelist first, then blacklist, then a hard-coded
  blocklist for catastrophic operations** (raw writes to `/dev/sd*`/`/dev/md*`,
  destructive `mdcmd` array commands, `mkfs`/`wipefs`/`shred` against block
  devices, `rm -rf /`, fork bombs, etc.) is checked on every command — in that
  order — and the hard blocklist cannot be overridden by your whitelist, even
  a maximally permissive one, nor by `allowAllCommands`. The one thing that
  removes it is the separate, deliberately dangerous `disableHardBlocklist`
  opt-in described below (default `false`).
- **The bearer token is generated randomly on first run.** There is no
  default token. Rotate it any time from the Settings page.
- **`config.json` is written with `0600` permissions**, and is never served
  by the webGUI without going through Unraid's own authenticated session.
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
- **`disableHardBlocklist` (default `false`) is a second, more dangerous
  opt-in that removes the hard-coded blocklist entirely** — the built-in
  safety net for catastrophic operations. It is deliberately a *separate*
  switch from `allowAllCommands`: opening the whitelist gate and removing
  the last backstop against an unrecoverable command are not the same
  decision, so enabling one never implies the other. With it on, only your
  own `commandBlacklist` stands between the bearer token and a command that
  wipes a disk, destroys the array, or formats the boot device. It exists
  for operators who need to run one of those blocked operations
  deliberately (e.g. formatting a new disk from this tool) and accept full
  responsibility. Enabling it is logged loudly at startup. Leave it `false`
  unless you specifically need it.
- **Output is capped at 1 MiB per stream (stdout/stderr).** A command that
  exceeds it is terminated immediately rather than left running until its
  timeout while consuming unbounded memory (e.g. `yes`, or `cat` on a huge
  file).
- **Every invocation is audit-logged** as a structured JSON line on stdout —
  the command text, outcome (`success`/`rejected`/`timeout`/`output_cap`/
  `error`), exit code, and duration — captured by the rc.d log file, with no
  extra configuration.
- **If you expose this outside your LAN** (e.g. via the plugin's built-in
  Cloudflare tunnel), **put it behind
  [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)**
  or equivalent so requests need an authenticated identity in addition to the
  bearer token. A tunnel alone only gets you a routable hostname, not
  authorization.

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
  `/boot` is durable). See `config.example.json` for the shape — it now
  ships with a curated starting-point whitelist/blacklist (safe, read-only
  commands like `uptime`, `docker ps`/`logs`/`inspect`, array/disk status,
  SMART data; explicit denials like `reboot`, `docker rm`/`stop`, `passwd`)
  to adapt rather than write from scratch. The Settings page has "Load
  example whitelist"/"Load example blacklist" buttons for the same
  template, plus explanatory text alongside every field (including exactly
  what the hard-coded safety blocklist covers).
- The MCP library's loopback DNS-rebinding protection (rejecting requests
  whose `Host` header isn't a localhost value) is disabled: cloudflared
  forwards tunneled requests to our `127.0.0.1` listener while preserving the
  original public hostname in `Host`, which that protection would otherwise
  reject outright. It's a mitigation for local desktop MCP servers reachable
  by a user's own browser, which doesn't apply to this headless-NAS setup;
  the bearer/path token is the real access control here, not the `Host`
  header.
- An optional `cloudflared` tunnel (quick, authorized/`local`, or
  managed/`named` mode — see "Cloudflare tunnel modes" above) exposes the
  server publicly. `rc.unraid-shell-mcp-cloudflared` reads tunnel settings
  from the same config file via `unraid-shell-mcp config-get <field>` (no
  JSON tooling needed in the shell script) and, in quick mode, parses the
  ephemeral `*.trycloudflare.com` URL out of the cloudflared log, writing it
  to `/var/run/unraid-shell-mcp-cloudflared-url.txt` for the Settings page.
  In authorized (`local`) mode it instead drives `cloudflared tunnel login`
  (surfacing the one-time authorization URL the same way, via
  `/var/run/unraid-shell-mcp-cloudflared-login-url.txt`), then `tunnel
  create` and `tunnel route dns`, persisting cloudflared's own state
  (`cert.pem`, the tunnel's credentials file, a generated ingress
  `config.yml`) under `/boot/config/plugins/unraid-shell-mcp/cloudflared/`
  so none of it needs redoing after a reboot. Every step that shells out to
  `cloudflared` runs as a tracked background child (not just backgrounded
  and forgotten) specifically so `stop()` can reliably kill whichever one
  is currently in flight — including a `tunnel login` that's still waiting
  on a human — instead of leaving it to run to completion as an orphan.
- **Boot-time autostart**: `plugin/event/started` is installed into the
  plugin's `event/` directory, which Unraid's `emhttp_event` runs
  automatically once the array has finished starting (every boot, and
  after any manual array stop/start). It calls each rc.d script's `start`
  gated on its own Settings-page toggle (`autostartMcp` /
  `autostartTunnel` in config.json, both on by default; a config file
  predating the toggles behaves as "on"). Each script safely no-ops if
  already running, and the tunnel one no-ops when `tunnelMode` is `off` —
  so with the toggles on, the MCP server and tunnel come back up on their
  own after a reboot.

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
  event/started           boot-time autostart hook, run by emhttp_event
webgui/UnraidShellMcp.page  Settings page (token, whitelist, tunnel, status)
.github/workflows/release.yml  builds + publishes the .txz/.plg (tag push
                          or manual workflow_dispatch with a version input)
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
make -C plugin/package VERSION=2026.07.24
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

- **The `makepkg`-proper packaging path is untested — but the fallback
  tar+xz path is the one actually proven in production.** The GitHub
  Actions runner that builds releases has no `makepkg`, so every published
  `.txz` comes from the Makefile's tar+xz fallback — and those packages
  have since been installed and upgraded repeatedly on a real Unraid
  7.3.2 server via the normal plugin manager (`upgradepkg`), working as
  intended. The inverse remains untested: building with real Slackware
  `makepkg` has never been exercised, since no build host with it has
  been available.
- **cloudflared behavior was originally developed against a fake binary,
  and one real-CLI difference slipped through exactly as that limitation
  warned.** This project's sandboxed development environment can't reach
  Cloudflare, so the quick/named/authorized-mode logic in
  `rc.unraid-shell-mcp-cloudflared` — mode switching, URL-file writing,
  clean process teardown, no leaked child processes, tunnel-id extraction —
  was verified against a fake `cloudflared` that reproduces the real CLI's
  output format. The fake couldn't catch flag-parsing differences: the
  real CLI rejects `--config` placed after the `run` subcommand, which
  made authorized mode exit instantly on every start until it was
  diagnosed on a live server and fixed (v0.1.11). Since then, authorized
  (`local`) mode **has** been verified end-to-end against a real Unraid
  7.3.2 server, real cloudflared, and a real Cloudflare account — login →
  create → route dns → run, through to a working public hostname — and
  the loopback-Host-header 403 interaction was likewise diagnosed and
  fixed against a live tunnel. Quick and named modes have been run
  against real tunnels too, but are not systematically re-verified on
  every change; the fake-binary suite remains the pre-release check.
- No Unraid Community Applications submission — install via the raw `.plg`
  URL above only.
- **Unraid runs two different version comparisons on updates, and both
  have quirks.** The plugin manager compares `.plg` versions with PHP's
  `strcmp` — plain lexicographic, so it considered `0.1.10` *older* than
  `0.1.9` and silently skipped that update ("not installing older
  version"; really happened here for 0.1.9 → 0.1.10/0.1.11). Its
  `upgradepkg` then separately compares installed package names with
  `sort -V` — which treats a trailing letter as *older* than none, so a
  `2026.07.24b` release was skipped as "newer version already installed"
  (also really happened; that version reported itself installed while the
  old files kept running). This project's versions are therefore
  date-based, digits and dots only: `YYYY.MM.DD`, with a numeric fourth
  component (`YYYY.MM.DD.N`) for further releases on the same day — a
  format both comparisons order correctly, which also sorts above every
  old `0.1.x` and lettered version, so updating from any earlier release
  works through the normal update path. If an update is ever wrongly
  skipped anyway: `plugin install <plg-url> forced` from a terminal
  bypasses the `.plg`-level check (and `upgradepkg --reinstall` the
  package-level one).
- **`cloudflared` is fetched over HTTPS from GitHub's "latest" release URL
  with no checksum or signature pinned in the `.plg` installer**, unlike
  this project's own `.txz` (which is md5-verified before `upgradepkg` ever
  runs). It only downloads once (skipped if the binary already exists) and
  runs as root. This is a deliberate, accepted tradeoff for now — pinning a
  specific version + hash would need to be kept in sync with upstream
  releases — rather than an oversight; tightening it is possible if that
  tradeoff stops being acceptable.
