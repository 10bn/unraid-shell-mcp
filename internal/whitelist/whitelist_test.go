package whitelist

import "testing"

func TestEmptyWhitelistFailsClosed(t *testing.T) {
	m, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	allowed, _ := m.Allowed("echo hello")
	if allowed {
		t.Fatal("expected empty whitelist to reject all commands")
	}
}

func TestWhitelistAllowsMatchingCommand(t *testing.T) {
	m, err := New([]string{`^echo\b`, `^uptime$`}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, reason := m.Allowed("echo hello"); !allowed {
		t.Fatalf("expected command to be allowed, got reason: %s", reason)
	}
	if allowed, _ := m.Allowed("uptime"); !allowed {
		t.Fatal("expected uptime to be allowed")
	}
}

func TestWhitelistRejectsNonMatchingCommand(t *testing.T) {
	m, err := New([]string{`^echo\b`}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, _ := m.Allowed("cat /etc/shadow"); allowed {
		t.Fatal("expected non-matching command to be rejected")
	}
}

func TestUserBlacklistOverridesWhitelist(t *testing.T) {
	m, err := New([]string{`.*`}, []string{`^cat\b`})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, _ := m.Allowed("cat /etc/passwd"); allowed {
		t.Fatal("expected blacklisted command to be rejected despite permissive whitelist")
	}
	if allowed, _ := m.Allowed("echo hi"); !allowed {
		t.Fatal("expected non-blacklisted command to still be allowed")
	}
}

func TestHardBlocklistCannotBeOverriddenByWhitelist(t *testing.T) {
	// A maximally permissive user whitelist must not defeat the hard-coded
	// blocklist for catastrophic operations.
	m, err := New([]string{`.*`}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	dangerous := []string{
		"dd if=/dev/zero of=/dev/sda",
		"echo oops > /dev/sdb",
		"mkfs.ext4 /dev/md1",
		"wipefs -a /dev/sda1",
		"mdcmd stop",
		"rm -rf /",
		"rm -fr /",
		"rm --no-preserve-root -rf /",
	}
	for _, cmd := range dangerous {
		if allowed, reason := m.Allowed(cmd); allowed {
			t.Errorf("expected hard blocklist to reject %q, but it was allowed", cmd)
		} else if reason != "blocked by hard-coded safety rule (non-configurable)" {
			t.Errorf("command %q rejected for wrong reason: %s", cmd, reason)
		}
	}
}

func TestHardBlocklistDoesNotFalsePositiveOnSafeCommands(t *testing.T) {
	m, err := New([]string{`.*`}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	safe := []string{
		"df -h",
		"ls -la /mnt/user",
		"docker ps",
		"cat /var/log/syslog",
		"rm /tmp/scratch.txt",
	}
	for _, cmd := range safe {
		if allowed, reason := m.Allowed(cmd); !allowed {
			t.Errorf("expected safe command %q to be allowed, rejected: %s", cmd, reason)
		}
	}
}

func TestInvalidRegexReturnsError(t *testing.T) {
	if _, err := New([]string{`(unclosed`}, nil); err == nil {
		t.Fatal("expected error for invalid whitelist regex")
	}
	if _, err := New(nil, []string{`(unclosed`}); err == nil {
		t.Fatal("expected error for invalid blacklist regex")
	}
}
