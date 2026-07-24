# Builds the unraid-shell-mcp Slackware package (.txz) used by the .plg
# installer. Layout:
#
#   root/                       package/install root, tarred up as-is
#     install/
#       slack-desc               package description shown by pkgtool
#       doinst.sh                post-extraction script (chmod on binary + rc.d)
#     etc/rc.d/                  populated by this Makefile from ../rc.d
#     usr/local/sbin/            populated by this Makefile with the built binary
#     usr/local/emhttp/plugins/  populated by this Makefile from ../../webgui and ../event
#                                 (event/started is what Unraid's emhttp_event calls on
#                                 every array start, so the server + tunnel come back up
#                                 after a reboot without a manual click in the webGUI)
#
# On a real Slackware/Unraid host `makepkg` (part of pkgtools) is used to
# produce a spec-correct .txz. On other build hosts (e.g. this repo's CI,
# which runs on ubuntu-latest) makepkg is unavailable, so this Makefile
# falls back to building the archive by hand with tar + xz, replicating the
# same layout makepkg would produce. This fallback has not been verified
# against a real Unraid install — see README.md's "Known limitations"
# section.

PKGNAME    := unraid-shell-mcp
VERSION    ?= 0.0.0-dev
ARCH       := x86_64
BUILD      ?= 1
PKG_SUFFIX := $(PKGNAME)-$(VERSION)-$(ARCH)-$(BUILD)

ROOT_DIR       := $(CURDIR)/root
RCD_SRC_DIR    := $(CURDIR)/../rc.d
EVENT_SRC_DIR  := $(CURDIR)/../event
WEBGUI_SRC_DIR := $(CURDIR)/../../webgui
GO_MODULE_DIR  := $(CURDIR)/../..
OUT_DIR        := $(CURDIR)/out

BINARY_SRC := $(GO_MODULE_DIR)/dist/unraid-shell-mcp

.PHONY: all binary stage package clean

all: package

binary:
	cd $(GO_MODULE_DIR) && \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="-s -w -X main.version=$(VERSION)" \
		-o dist/unraid-shell-mcp ./cmd/unraid-shell-mcp

stage: binary
	install -d $(ROOT_DIR)/usr/local/sbin
	install -d $(ROOT_DIR)/etc/rc.d
	install -d $(ROOT_DIR)/usr/local/emhttp/plugins/unraid-shell-mcp
	install -d $(ROOT_DIR)/usr/local/emhttp/plugins/unraid-shell-mcp/event
	install -m 0755 $(BINARY_SRC) $(ROOT_DIR)/usr/local/sbin/unraid-shell-mcp
	install -m 0755 $(RCD_SRC_DIR)/rc.unraid-shell-mcp $(ROOT_DIR)/etc/rc.d/rc.unraid-shell-mcp
	install -m 0755 $(RCD_SRC_DIR)/rc.unraid-shell-mcp-cloudflared $(ROOT_DIR)/etc/rc.d/rc.unraid-shell-mcp-cloudflared
	install -m 0644 $(WEBGUI_SRC_DIR)/UnraidShellMcp.page $(ROOT_DIR)/usr/local/emhttp/plugins/unraid-shell-mcp/UnraidShellMcp.page
	install -m 0755 $(EVENT_SRC_DIR)/started $(ROOT_DIR)/usr/local/emhttp/plugins/unraid-shell-mcp/event/started
	chmod 0755 $(ROOT_DIR)/install/doinst.sh

package: stage
	mkdir -p $(OUT_DIR)
	if command -v makepkg >/dev/null 2>&1; then \
		echo "Using real makepkg"; \
		( cd $(ROOT_DIR) && makepkg -l y -c n $(OUT_DIR)/$(PKG_SUFFIX).txz ); \
	else \
		echo "makepkg not found; falling back to manual tar+xz packaging (see Makefile header)"; \
		( cd $(ROOT_DIR) && tar --numeric-owner --owner=0 --group=0 -cf - . | xz -6 > $(OUT_DIR)/$(PKG_SUFFIX).txz ); \
	fi
	cd $(OUT_DIR) && md5sum $(PKG_SUFFIX).txz > $(PKG_SUFFIX).txz.md5
	@echo "Built $(OUT_DIR)/$(PKG_SUFFIX).txz"

clean:
	rm -rf $(ROOT_DIR)/usr $(ROOT_DIR)/etc $(OUT_DIR)
	rm -rf $(GO_MODULE_DIR)/dist
