package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultGeneratesRandomToken(t *testing.T) {
	a, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	b, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if a.BearerToken == "" {
		t.Fatal("expected non-empty bearer token")
	}
	if a.BearerToken == b.BearerToken {
		t.Fatal("expected two Default() calls to generate different tokens")
	}
	if len(a.CommandWhitelist) != 0 {
		t.Fatal("expected fail-closed default: no whitelist entries")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	cfg.CommandWhitelist = []string{`^echo\b`}
	cfg.TunnelMode = TunnelModeQuick

	if err := cfg.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected config file perms 0600, got %o", perm)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.BearerToken != cfg.BearerToken {
		t.Errorf("token mismatch after round trip")
	}
	if len(loaded.CommandWhitelist) != 1 || loaded.CommandWhitelist[0] != `^echo\b` {
		t.Errorf("whitelist mismatch after round trip: %v", loaded.CommandWhitelist)
	}
	if loaded.TunnelMode != TunnelModeQuick {
		t.Errorf("tunnel mode mismatch: %v", loaded.TunnelMode)
	}
}

func TestLoadOrCreateGeneratesFreshConfigWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nested", "config.json")

	cfg, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.BearerToken == "" {
		t.Fatal("expected generated token")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}

	// Loading again should return the same persisted token, not regenerate.
	cfg2, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("LoadOrCreate second call: %v", err)
	}
	if cfg2.BearerToken != cfg.BearerToken {
		t.Fatal("expected LoadOrCreate to persist and reuse the same token")
	}
}
