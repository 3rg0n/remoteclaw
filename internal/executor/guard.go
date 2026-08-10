package executor

import (
	"path/filepath"
	"strings"
)

// Guard enforces the path half of the config/secret lockdown: when enabled it
// hard-denies the file tools (read_file, write_file, list_dir) on protected
// paths. The authoritative denial is the OS (config/secrets are owned by root
// and not readable by the low-privilege service account); this guard is the
// in-process defense-in-depth layer on top of that. See ADR 0003 and
// THREAT_MODEL.md.
//
// Command-string matching for secret reads is not here — it is one group of
// rules in security.CommandPolicy, alongside the destructive-command rules, so
// there is a single deny-list engine (ADR 0006). This type owns path
// canonicalization and hands its canonicalized paths to that policy.
type Guard struct {
	enabled        bool
	protectedPaths []string // canonicalized absolute paths
}

// NewGuard builds a lockdown guard. protectedPaths are files/dirs the agent's
// own tools must never read or modify (config dir, .env, pass store, install
// dir). When enabled is false the guard permits everything (the operator's
// explicit opt-out to "wide open").
func NewGuard(enabled bool, protectedPaths []string) *Guard {
	canon := make([]string, 0, len(protectedPaths))
	for _, p := range protectedPaths {
		if p == "" {
			continue
		}
		canon = append(canon, canonPath(p))
	}
	return &Guard{
		enabled:        enabled,
		protectedPaths: canon,
	}
}

// Enabled reports whether the lockdown is active.
func (g *Guard) Enabled() bool {
	return g != nil && g.enabled
}

// ProtectedPaths returns the canonicalized protected paths, for the command
// policy's protected-path rule. Empty when the guard is disabled, so a
// disabled lockdown yields no secret-read rules.
func (g *Guard) ProtectedPaths() []string {
	if !g.Enabled() {
		return nil
	}
	out := make([]string, len(g.protectedPaths))
	copy(out, g.protectedPaths)
	return out
}

// canonPath resolves a path to a cleaned absolute form, following symlinks
// where the target exists, to prevent symlink/relative-path bypasses.
func canonPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// IsProtectedPath reports whether path is inside or equal to any protected
// path. Paths are canonicalized (abs + symlink-resolved) before comparison so
// relative paths and symlinks cannot bypass the check.
func (g *Guard) IsProtectedPath(path string) bool {
	if !g.Enabled() {
		return false
	}
	target := canonPath(path)
	for _, prot := range g.protectedPaths {
		rel, err := filepath.Rel(prot, target)
		if err != nil {
			// Different volumes (Windows) → cannot be inside; skip.
			continue
		}
		if rel == "." || !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
