// Command unraid-shell-mcp runs an MCP server that executes shell commands
// on the local Unraid host, gated by a bearer token and a fail-closed
// command whitelist.
package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/10bn/unraid-shell-mcp/internal/auth"
	"github.com/10bn/unraid-shell-mcp/internal/config"
	"github.com/10bn/unraid-shell-mcp/internal/mcp"
	"github.com/10bn/unraid-shell-mcp/internal/whitelist"
)

// configGetFields lists the config fields the "config-get" subcommand may
// print. It exists so rc.d shell scripts (no JSON tooling on stock Unraid)
// can read tunnel settings without parsing config.json themselves. The
// bearer token is deliberately excluded here; the webGUI reads it directly
// via PHP's json_decode on the same host.
var configGetFields = map[string]func(*config.Config) string{
	"listenAddr":               func(c *config.Config) string { return c.ListenAddr },
	"tunnelMode":               func(c *config.Config) string { return string(c.TunnelMode) },
	"cloudflareTunnelToken":    func(c *config.Config) string { return c.CloudflareTunnelToken },
	"cloudflareTunnelHostname": func(c *config.Config) string { return c.CloudflareTunnelHostname },
}

const allowAllCommandsWarning = `
################################################################
# WARNING: allowAllCommands is enabled in config.json.
# The commandWhitelist is NOT being enforced — every command is
# eligible to run except the hard-coded safety blocklist and any
# commandBlacklist entries. Anyone holding the bearer token has
# full shell access to this machine.
#
# This is an explicit, deliberate opt-in. If that wasn't your
# intent, set "allowAllCommands": false in config.json and
# restart the service.
################################################################`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "config-get" {
		runConfigGet(os.Args[2:])
		return
	}

	configPath := flag.String("config", config.DefaultPath, "path to config.json")
	listenAddr := flag.String("listen", "", "override the listen address from config.json")
	token := flag.String("token", "", "override the bearer token from config.json")
	flag.Parse()

	cfg, err := config.LoadOrCreate(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if *token != "" {
		cfg.BearerToken = *token
	}
	if cfg.BearerToken == "" {
		log.Fatal("no bearer token configured; refusing to start unauthenticated")
	}

	matcher, err := whitelist.New(cfg.CommandWhitelist, cfg.CommandBlacklist, cfg.AllowAllCommands)
	if err != nil {
		log.Fatalf("invalid command whitelist/blacklist: %v", err)
	}
	switch {
	case cfg.AllowAllCommands:
		log.Print(allowAllCommandsWarning)
	case len(cfg.CommandWhitelist) == 0:
		log.Printf("warning: commandWhitelist is empty; all execute-command calls will be rejected until it is configured")
	}

	auditLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	mcpSrv := mcp.New(matcher, auditLogger)
	streamable := mcpserver.NewStreamableHTTPServer(mcpSrv)

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(cfg.BearerToken, streamable))

	log.Printf("unraid-shell-mcp listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runConfigGet implements `unraid-shell-mcp config-get [-config path] <field>`,
// printing the requested field's raw value to stdout. Used by rc.d scripts
// that need tunnel settings without a JSON parser available.
func runConfigGet(args []string) {
	fs := flag.NewFlagSet("config-get", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultPath, "path to config.json")
	fs.Parse(args)

	field := fs.Arg(0)
	getter, ok := configGetFields[field]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown config-get field: %q\n", field)
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(getter(cfg))
}
