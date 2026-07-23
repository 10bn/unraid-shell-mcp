// Package whitelist decides whether a shell command is allowed to run.
//
// The policy is fail-closed and defense-in-depth:
//
//  1. hardBlocklist is checked first and can never be overridden by user
//     configuration. It exists to stop catastrophic operations (raw disk
//     writes, array destruction, wiping the filesystem) even if an operator
//     misconfigures the whitelist.
//  2. The user-supplied blacklist is checked next.
//  3. The command must then match at least one user-supplied whitelist
//     pattern. An empty whitelist allows nothing — there is no "empty
//     whitelist means allow everything" fallback.
package whitelist

import (
	"fmt"
	"regexp"
)

// hardBlocklistPatterns can never be overridden by user configuration.
// These target operations that would destroy the array, the boot device,
// or the filesystem wholesale.
var hardBlocklistPatterns = []string{
	// Direct writes to block devices (disks, array members, cache, USB boot).
	`\bdd\b.*\bof=\s*/dev/(sd|nvme|md|hd|xvd)`,
	`>\s*/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\bmkfs(\.\S+)?\s+.*/dev/(sd|nvme|md|hd|xvd)`,
	`\bwipefs\b`,
	`\bshred\b.*/dev/(sd|nvme|md|hd|xvd)`,
	`\bblkdiscard\b`,
	// Unraid array control: stopping/starting/destroying the array or
	// triggering a parity rebuild from a shell command bypasses the
	// safety checks the webGUI performs.
	`\bmdcmd\b\s+(stop|nocheck|clear)`,
	// Recursive force-delete of root or wide, unqualified filesystem roots.
	`\brm\s+.*-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*\s+/\s*($|[;&|])`,
	`\brm\s+.*-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*\s+/\s*($|[;&|])`,
	`\brm\s+.*-[a-zA-Z]*r[a-zA-Z]*\s+/\*`,
	`\brm\s+.*--no-preserve-root`,
	`:\(\)\s*\{\s*:\|\s*:\s*&\s*\}\s*;\s*:`, // fork bomb
}

// Matcher evaluates commands against the hard blocklist plus a
// user-configured whitelist/blacklist pair.
type Matcher struct {
	hardBlocklist []*regexp.Regexp
	blacklist     []*regexp.Regexp
	whitelist     []*regexp.Regexp
}

// New compiles the user-supplied whitelist and blacklist patterns together
// with the built-in hard blocklist. It returns an error naming the first
// invalid regular expression encountered.
func New(userWhitelist, userBlacklist []string) (*Matcher, error) {
	hard, err := compileAll(hardBlocklistPatterns)
	if err != nil {
		return nil, fmt.Errorf("internal hard blocklist pattern: %w", err)
	}
	wl, err := compileAll(userWhitelist)
	if err != nil {
		return nil, fmt.Errorf("commandWhitelist: %w", err)
	}
	bl, err := compileAll(userBlacklist)
	if err != nil {
		return nil, fmt.Errorf("commandBlacklist: %w", err)
	}
	return &Matcher{hardBlocklist: hard, blacklist: bl, whitelist: wl}, nil
}

func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// Allowed reports whether command may be executed, and if not, why.
func (m *Matcher) Allowed(command string) (bool, string) {
	for _, re := range m.hardBlocklist {
		if re.MatchString(command) {
			return false, "blocked by hard-coded safety rule (non-configurable)"
		}
	}
	for _, re := range m.blacklist {
		if re.MatchString(command) {
			return false, "blocked by commandBlacklist"
		}
	}
	if len(m.whitelist) == 0 {
		return false, "no commandWhitelist configured; fail-closed default rejects all commands"
	}
	for _, re := range m.whitelist {
		if re.MatchString(command) {
			return true, ""
		}
	}
	return false, "command does not match any commandWhitelist pattern"
}
