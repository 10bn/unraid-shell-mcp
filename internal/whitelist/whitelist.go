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
// A second, separate opt-in, disableHardBlocklist, removes step 1 as well —
// the built-in catastrophic-operation safety net. It is deliberately its
// own flag, distinct from allowAllCommands, so that "let everything through
// the whitelist gate" and "remove the last backstop against wiping a disk"
// are never the same decision. It defaults off; turning it on means raw
// block-device writes, mkfs/wipefs, array-destroying mdcmd commands,
// rm -rf /, and fork bombs can all run if they otherwise pass. Only step 2,
// the user-supplied blacklist, then stands between the bearer token and an
// unrecoverable command. It exists for operators who need to run one of the
// blocked operations deliberately (e.g. formatting a new disk from this
// tool) and accept full responsibility for the consequences.
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
	// Direct writes to block devices (disks, array members, cache, USB boot),
	// via the usual suspects: dd, shell redirection, mkfs, and other
	// utilities whose job is (or can be used as) writing arbitrary data to
	// a raw device path.
	`\bdd\b.*\bof=\s*/dev/(sd|nvme|md|hd|xvd)`,
	`\bdcfldd\b`,
	`>\s*/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\btee\b.*\s/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\bcp\b.*\s/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\s*($|[;&|])`,
	`\binstall\b.*\s/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\brsync\b.*\s/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\bsocat\b.*/dev/(sd|nvme|md|hd|xvd)[a-z0-9]*\b`,
	`\bmkfs(\.\S+)?\s+.*/dev/(sd|nvme|md|hd|xvd)`,
	`\bwipefs\b`,
	`\bshred\b.*/dev/(sd|nvme|md|hd|xvd)`,
	`\bblkdiscard\b`,
	// Unraid array control: stopping/starting/destroying the array or
	// triggering a parity rebuild from a shell command bypasses the
	// safety checks the webGUI performs.
	`\bmdcmd\b\s+(stop|nocheck|clear)`,
	`:\(\)\s*\{\s*:\|\s*:\s*&\s*\}\s*;\s*:`, // fork bomb
}

// rmInvocation finds each "rm ..." invocation within a command, scoped to
// the next shell separator, so a multi-command line is checked segment by
// segment rather than treating the whole line as one bag of flags.
var rmInvocation = regexp.MustCompile(`\brm\b[^;&|\n]*`)

// rmRecursiveFlag and rmForceFlag detect a recursive/force flag in any of
// its forms: a short combined cluster (-r, -rf, -fr, -Rf, ...) or the GNU
// long option (--recursive, --force). Go's regexp (RE2) has no lookahead,
// so "contains a recursive flag AND a force flag AND a destructive target"
// can't be expressed as a single pattern the way the hard blocklist's other
// entries are; checking each condition independently against the same
// rm-scoped segment gets the same effect. This also closes a gap the
// previous single combined-cluster regex had: it only matched short options
// like -rf/-fr, so `rm --recursive --force /` (functionally identical to
// `rm -rf /`) passed through unblocked.
var rmRecursiveFlag = regexp.MustCompile(`-[a-zA-Z]*[rR][a-zA-Z]*\b|--recursive\b`)
var rmForceFlag = regexp.MustCompile(`-[a-zA-Z]*f[a-zA-Z]*\b|--force\b`)
var rmNoPreserveRoot = regexp.MustCompile(`--no-preserve-root\b`)

// rmBareRootTarget matches a bare "/" as a standalone argument (not merely
// a path that starts with "/", like "/mnt/user/tmp").
var rmBareRootTarget = regexp.MustCompile(`(^|\s)/(\s|$)`)

// rmWildcardRootTarget matches "/*" as a standalone argument. Unlike a bare
// "/" (which GNU coreutils' rm refuses to touch unless --no-preserve-root
// is also given, as a built-in safety net independent of this blocklist),
// "/*" is expanded by the shell into a list of top-level directories before
// rm ever sees it, so that built-in protection does not apply — recursively
// deleting it is exactly as destructive with or without an explicit force
// flag, since a non-interactive rm proceeds without prompting regardless
// once given -r.
var rmWildcardRootTarget = regexp.MustCompile(`(^|\s)/\*(\s|$)`)

// isRecursiveForceRootDelete reports whether command contains an "rm"
// invocation that would recursively wipe out "/" or everything under it,
// regardless of flag order or short-vs-long-option form.
func isRecursiveForceRootDelete(command string) bool {
	for _, seg := range rmInvocation.FindAllString(command, -1) {
		if !rmRecursiveFlag.MatchString(seg) {
			continue
		}
		if rmWildcardRootTarget.MatchString(seg) {
			return true
		}
		if rmBareRootTarget.MatchString(seg) && (rmForceFlag.MatchString(seg) || rmNoPreserveRoot.MatchString(seg)) {
			return true
		}
	}
	return false
}

// Matcher evaluates commands against the hard blocklist plus a
// user-configured whitelist/blacklist pair.
type Matcher struct {
	hardBlocklist    []*regexp.Regexp
	blacklist        []*regexp.Regexp
	whitelist        []*regexp.Regexp
	allowAll         bool
	disableHardBlock bool
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
//
// disableHardBlocklist is the config.json "disableHardBlocklist" opt-in:
// when true, the built-in catastrophic-operation blocklist (step 1) is not
// applied at all. This is a separate, deliberately independent flag from
// allowAll; see the package doc for the consequences.
func New(userWhitelist, userBlacklist []string, allowAll, disableHardBlocklist bool) (*Matcher, error) {
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
	return &Matcher{
		hardBlocklist:    hard,
		blacklist:        bl,
		whitelist:        wl,
		allowAll:         allowAll,
		disableHardBlock: disableHardBlocklist,
	}, nil
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
	if !m.disableHardBlock {
		if isRecursiveForceRootDelete(command) {
			return false, "blocked by hard-coded safety rule (non-configurable)"
		}
		for _, re := range m.hardBlocklist {
			if re.MatchString(command) {
				return false, "blocked by hard-coded safety rule (non-configurable)"
			}
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
