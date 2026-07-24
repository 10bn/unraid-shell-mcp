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
5. Point your MCP client at `http://<unraid-ip>:8483/mcp` (or your tunnel
   hostname) with header `Authorization: Bearer <token>`. If your client has
   no way to set a custom header, use `.../mcp/<token>` instead (same
   token, embedded in the URL) and select "no authentication" — the
   Settings page shows this exact URL ready to copy. See "Two ways to
   authenticate" below.

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

## Security

**Whoever holds the bearer token gets full shell access to this machine** —
the array, Docker, VMs, every share. Treat the token like a root password,
because it effectively is one.

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
  `/boot` is durable). See `config.example.json` for the shape.
- The MCP library's loopback DNS-rebinding protection (rejecting requests
  whose `Host` header isn't a localhost value) is disabled: cloudflared
  forwards tunneled requests to our `127.0.0.1` listener while preserving the
  original public hostname in `Host`, which that protection would otherwise
  reject outright. It's a mitigation for local desktop MCP servers reachable
  by a user's own browser, which doesn't apply to this headless-NAS setup;
  the bearer/path token is the real access control here, not the `Host`
  header.
- An optional `cloudflared` tunnel (quick or named mode) exposes the server
  publicly. `rc.unraid-shell-mcp-cloudflared` reads tunnel settings from the
  same config file via `unraid-shell-mcp config-get <field>` (no JSON
  tooling needed in the shell script) and, in quick mode, parses the
  ephemeral `*.trycloudflare.com` URL out of the cloudflared log, writing it
  to `/var/run/unraid-shell-mcp-cloudflared-url.txt` for the Settings page.

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
.github/workflows/release.yml  builds + publishes the .txz/.plg on tag push
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
  `cloudflared` binary directly, and blocks reaching `*.trycloudflare.com`
  URLs to test a live tunnel). The quick/named-mode logic in
  `rc.unraid-shell-mcp-cloudflared` — mode switching, URL-file writing, clean
  process teardown, no leaked child processes — was verified against a fake
  `cloudflared` binary that reproduces the real log format, and the
  loopback-Host-header interaction with a real tunnel was diagnosed and
  fixed based on a live user report, but end-to-end tunnel behavior has not
  been directly exercised from this sandbox.
- No Unraid Community Applications submission — install via the raw `.plg`
  URL above only.
- **`cloudflared` is fetched over HTTPS from GitHub's "latest" release URL
  with no checksum or signature pinned in the `.plg` installer**, unlike
  this project's own `.txz` (which is md5-verified before `upgradepkg` ever
  runs). It only downloads once (skipped if the binary already exists) and
  runs as root. This is a deliberate, accepted tradeoff for now — pinning a
  specific version + hash would need to be kept in sync with upstream
  releases — rather than an oversight; tightening it is possible if that
  tradeoff stops being acceptable.
