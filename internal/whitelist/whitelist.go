// Package whitelist decides whether a shell command is allowed to run.
//
// The policy is fail-closed and defense-in-depth:
//
//  1. hardBlocklist is checked first and can never be overridden by user
//     configuration. It exists to stop catastrophic operations (raw disk
//     writes, array destruction, wiping the filesystem) even if an operator
//     misconfigures the whitelist. It uses substring semantics (matches if
//     the pattern appears anywhere in the command) so it still catches a
//     dangerous fragment tucked after a `;` or `&&` in a longer command.
//  2. The user-supplied blacklist is checked next, with the same substring
//     semantics as the hard blocklist, for the same reason.
//  3. The command must then fully match at least one user-supplied
//     whitelist pattern — the pattern must account for the entire command
//     string, not just a prefix of it. An empty whitelist allows nothing —
//     there is no "empty whitelist means allow everything" fallback.
//
// An operator can explicitly opt into skipping step 3 entirely via the
// allowAllCommands config flag (see New), but steps 1 and 2 still always
// apply — allowAllCommands widens the whitelist gate, it does not disable
// the blocklists.
//
// Whitelist patterns require a full match rather than "appears somewhere in
// the command" specifically to prevent injection via shell metacharacters:
// since commands run through /bin/sh -c, a whitelist entry like `^echo\b`
// under substring semantics would also match (and therefore execute in
// full) "echo hi; rm -rf /etc/shadow", because MatchString only checks that
// "echo" appears at the start — it does not care what follows. Requiring a
// full match means the whitelist pattern itself must account for every
// character the operator intends to allow (e.g. `^echo\b.*$` or an exact
// `^echo hi$`).
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
	allowAll      bool
}

// New compiles the user-supplied whitelist and blacklist patterns together
// with the built-in hard blocklist. It returns an error naming the first
// invalid regular expression encountered.
//
// allowAll is the config.json "allowAllCommands" opt-in: when true, the
// commandWhitelist requirement is skipped entirely (any command not
// matching the hard blocklist or commandBlacklist is allowed). It never
// bypasses those two — they are defense-in-depth precisely so that even a
// wide-open whitelist policy still can't run a catastrophic command.
func New(userWhitelist, userBlacklist []string, allowAll bool) (*Matcher, error) {
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
	return &Matcher{hardBlocklist: hard, blacklist: bl, whitelist: wl, allowAll: allowAll}, nil
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
	if m.allowAll {
		return true, ""
	}
	if len(m.whitelist) == 0 {
		return false, "no commandWhitelist configured; fail-closed default rejects all commands"
	}
	for _, re := range m.whitelist {
		if fullMatch(re, command) {
			return true, ""
		}
	}
	return false, "command does not fully match any commandWhitelist pattern"
}

// fullMatch reports whether re's match spans command's entire length,
// rather than merely appearing somewhere within it. Go's regexp package,
// unlike e.g. Python's re.fullmatch, has no built-in full-match mode, so
// this checks that the leftmost match starts at 0 and ends at len(command).
func fullMatch(re *regexp.Regexp, command string) bool {
	loc := re.FindStringIndex(command)
	return loc != nil && loc[0] == 0 && loc[1] == len(command)
}
