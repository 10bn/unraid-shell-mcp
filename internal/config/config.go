// Package config loads and saves the unraid-shell-mcp configuration file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath is where Unraid persists plugin config across reboots
// (the /boot overlay is the only writable location that survives a reboot).
const DefaultPath = "/boot/config/plugins/unraid-shell-mcp/config.json"

// DefaultListenAddr is the address the MCP HTTP server binds to.
const DefaultListenAddr = "127.0.0.1:8483"

// TunnelMode selects how (or whether) cloudflared exposes the server.
type TunnelMode string

const (
	TunnelModeOff   TunnelMode = "off"
	TunnelModeQuick TunnelMode = "quick"
	TunnelModeNamed TunnelMode = "named"
	// TunnelModeLocal is a "locally-managed tunnel" in Cloudflare's own
	// terminology: no token is pasted from the dashboard. Instead
	// rc.unraid-shell-mcp-cloudflared runs `cloudflared tunnel login`
	// itself, surfaces the one-time authorization URL it prints for the
	// Settings page to show as a clickable link, and once that completes
	// (cert.pem appears) creates the tunnel, routes cloudflareTunnelHostname
	// to it, and runs it — all driven by CloudflareTunnelHostname, the same
	// field TunnelModeNamed uses, with no separate token needed.
	TunnelModeLocal TunnelMode = "local"
)

// Config is the on-disk shape of config.json.
type Config struct {
	// BearerToken must be presented as "Authorization: Bearer <token>" on
	// every MCP request. Generated randomly on first install; never
	// defaulted to a fixed value.
	BearerToken string `json:"bearerToken"`

	// ListenAddr is the address the HTTP/MCP server binds to.
	ListenAddr string `json:"listenAddr"`

	// CommandWhitelist is a list of regular expressions. A command must
	// match at least one entry to be eligible for execution. An empty
	// whitelist means nothing is allowed to run (fail closed).
	CommandWhitelist []string `json:"commandWhitelist"`

	// CommandBlacklist is a list of regular expressions checked in addition
	// to the whitelist; a match rejects the command even if the whitelist
	// would otherwise allow it. This is user-configurable, layered on top
	// of (never a replacement for) the hard-coded blocklist in the
	// whitelist package.
	CommandBlacklist []string `json:"commandBlacklist"`

	// AllowAllCommands is an explicit opt-in that bypasses the
	// commandWhitelist requirement entirely (every command is eligible to
	// run). It does NOT bypass the hard-coded blocklist or
	// CommandBlacklist — those still apply. Defaults to false: a fresh
	// install stays fail-closed until an operator deliberately sets this
	// to true (there is no way to end up here by omission, since the zero
	// value is false). Enabling it is logged loudly at startup.
	AllowAllCommands bool `json:"allowAllCommands"`

	// TunnelMode selects how cloudflared exposes the server: "off",
	// "quick" (ephemeral trycloudflare.com URL, no account needed), or
	// "named" (stable hostname via a Cloudflare Zero Trust tunnel token).
	TunnelMode TunnelMode `json:"tunnelMode"`

	// CloudflareTunnelToken is the tunnel token from the Cloudflare Zero
	// Trust dashboard. Only used when TunnelMode is "named".
	CloudflareTunnelToken string `json:"cloudflareTunnelToken"`

	// CloudflareTunnelHostname is the public hostname configured for the
	// named tunnel. Only used when TunnelMode is "named".
	CloudflareTunnelHostname string `json:"cloudflareTunnelHostname"`
}

// path returns the config file path, defaulting to DefaultPath.
func path(configPath string) string {
	if configPath == "" {
		return DefaultPath
	}
	return configPath
}

// Load reads the config file at configPath (or DefaultPath if empty).
// It does not create the file if missing; callers that want a fresh
// default config should check os.IsNotExist and call Default().
func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(path(configPath))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if cfg.TunnelMode == "" {
		cfg.TunnelMode = TunnelModeOff
	}
	return &cfg, nil
}

// LoadOrCreate loads the config file, generating a fresh one with a random
// bearer token if it does not yet exist.
func LoadOrCreate(configPath string) (*Config, error) {
	cfg, err := Load(configPath)
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	cfg, genErr := Default()
	if genErr != nil {
		return nil, genErr
	}
	if err := cfg.Save(configPath); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Default returns a fail-closed configuration with a freshly generated
// random bearer token and no whitelist entries (so no commands are
// executable until the operator configures one).
func Default() (*Config, error) {
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	return &Config{
		BearerToken:      token,
		ListenAddr:       DefaultListenAddr,
		CommandWhitelist: nil,
		CommandBlacklist: nil,
		AllowAllCommands: false,
		TunnelMode:       TunnelModeOff,
	}, nil
}

// generateToken returns a random 256-bit hex-encoded token.
func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Save writes the config to configPath (or DefaultPath if empty) with 0600
// permissions, creating parent directories as needed.
func (c *Config) Save(configPath string) error {
	p := path(configPath)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}
