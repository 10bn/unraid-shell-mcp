package whitelist

import "testing"

func TestEmptyWhitelistFailsClosed(t *testing.T) {
	m, err := New(nil, nil, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	allowed, _ := m.Allowed("echo hello")
	if allowed {
		t.Fatal("expected empty whitelist to reject all commands")
	}
}

func TestWhitelistAllowsMatchingCommand(t *testing.T) {
	m, err := New([]string{`^echo\b.*$`, `^uptime$`}, nil, false)
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
	m, err := New([]string{`^echo\b.*$`}, nil, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, _ := m.Allowed("cat /etc/shadow"); allowed {
		t.Fatal("expected non-matching command to be rejected")
	}
}

func TestWhitelistRequiresFullMatchNotPrefix(t *testing.T) {
	// A whitelist entry anchored only at the start (a common mistake) must
	// not allow shell metacharacters to smuggle in a second, unintended
	// command after the part the pattern actually describes.
	m, err := New([]string{`^echo\b`}, nil, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, reason := m.Allowed("echo hi; cat /etc/shadow"); allowed {
		t.Fatalf("expected injected command to be rejected under full-match semantics, reason: %s", reason)
	}
	// The pattern still allows the one exact string it fully describes.
	if allowed, reason := m.Allowed("echo"); !allowed {
		t.Fatalf("expected exact match to be allowed, got reason: %s", reason)
	}
}

func TestUserBlacklistOverridesWhitelist(t *testing.T) {
	m, err := New([]string{`.*`}, []string{`^cat\b`}, false)
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
	m, err := New([]string{`.*`}, nil, false)
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
	m, err := New([]string{`.*`}, nil, false)
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
	if _, err := New([]string{`(unclosed`}, nil, false); err == nil {
		t.Fatal("expected error for invalid whitelist regex")
	}
	if _, err := New(nil, []string{`(unclosed`}, false); err == nil {
		t.Fatal("expected error for invalid blacklist regex")
	}
}

func TestAllowAllCommandsBypassesEmptyWhitelist(t *testing.T) {
	m, err := New(nil, nil, true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, reason := m.Allowed("cat /etc/passwd"); !allowed {
		t.Fatalf("expected allowAllCommands to allow an unlisted command, got reason: %s", reason)
	}
}

func TestAllowAllCommandsStillBlockedByHardBlocklist(t *testing.T) {
	// The opt-in widens the whitelist gate; it must never reach the
	// hard-coded blocklist for catastrophic operations.
	m, err := New(nil, nil, true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, reason := m.Allowed("rm -rf /"); allowed {
		t.Fatalf("expected hard blocklist to still reject command under allowAllCommands, got: %s", reason)
	}
}

func TestAllowAllCommandsStillBlockedByUserBlacklist(t *testing.T) {
	m, err := New(nil, []string{`^cat\b`}, true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if allowed, reason := m.Allowed("cat /etc/shadow"); allowed {
		t.Fatalf("expected commandBlacklist to still apply under allowAllCommands, got: %s", reason)
	}
	if allowed, reason := m.Allowed("echo hi"); !allowed {
		t.Fatalf("expected non-blacklisted command to be allowed, got reason: %s", reason)
	}
}
