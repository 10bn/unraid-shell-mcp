#!/bin/sh
# doinst.sh - executed by pkgtools (upgradepkg/installpkg) immediately after
# this package's files are extracted onto the live filesystem. Paths below
# are relative to the package/install root (i.e. relative to /).

chmod 755 etc/rc.d/rc.unraid-shell-mcp
chmod 755 etc/rc.d/rc.unraid-shell-mcp-cloudflared
chmod 755 usr/local/sbin/unraid-shell-mcp
chmod 755 usr/local/emhttp/plugins/unraid-shell-mcp/event/started
