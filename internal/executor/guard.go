package executor

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Guard enforces the config/secret lockdown. When enabled it hard-denies the
// file tools (read_file, write_file, list_dir) on protected paths — the
// authoritative denial is the OS (config/secrets are owned by root and not
// readable by the low-privilege service account), and this guard is the
// in-process defense-in-depth layer on top of that.
//
// For execute_command the guard applies a best-effort pattern deny of obvious
// secret-reading commands. This is NOT an airtight boundary: a process that can
// run arbitrary shell can evade any in-process pattern match (base64, indirect
// reads, copy-then-read). The real protection for execute_command is the OS
// file permission that denies the service account read access regardless of the
// command. See ADR 0003 and THREAT_MODEL.md.
type Guard struct {
	enabled        bool
	protectedPaths []string // canonicalized absolute paths
	protectedRe    *regexp.Regexp
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
		protectedRe:    buildSecretReadRegex(canon),
	}
}

// Enabled reports whether the lockdown is active.
func (g *Guard) Enabled() bool {
	return g != nil && g.enabled
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

// IsSecretReadCommand best-effort detects a shell command that appears to read
// a protected file or dump the environment/secret store. Returns (true, reason)
// when the command matches. This is defense-in-depth only — see the Guard doc.
func (g *Guard) IsSecretReadCommand(command string) (bool, string) {
	if !g.Enabled() || g.protectedRe == nil {
		return false, ""
	}
	lc := strings.ToLower(command)

	// Password-store / GPG secret access.
	if strings.Contains(lc, "pass show") || strings.Contains(lc, "pass ls") ||
		strings.Contains(lc, "password-store") {
		return true, "reads the password store"
	}
	if strings.Contains(lc, "gpg -d") || strings.Contains(lc, "gpg --decrypt") {
		return true, "decrypts a secret with gpg"
	}

	// Environment dumps (may expose secrets loaded into the process env).
	if regexp.MustCompile(`(?i)\b(printenv|env|set)\b`).MatchString(lc) &&
		!strings.Contains(lc, "setup") { // avoid trivial false positives like "setup"
		if strings.Contains(lc, "token") || strings.Contains(lc, "secret") ||
			strings.Contains(lc, "challenge") || strings.Contains(lc, "key") ||
			strings.TrimSpace(lc) == "env" || strings.TrimSpace(lc) == "printenv" ||
			strings.TrimSpace(lc) == "set" {
			return true, "dumps environment variables that may contain secrets"
		}
	}
	if strings.Contains(lc, "/proc/self/environ") || strings.Contains(lc, "$env:") {
		return true, "reads the process environment"
	}

	// Direct reference to a known secret-bearing environment variable, in either
	// $VAR / ${VAR} (POSIX) or $env:VAR (PowerShell) form.
	if secretEnvRefRe.MatchString(command) {
		return true, "references a secret environment variable"
	}

	// Direct read of a protected path via a command.
	if g.protectedRe.MatchString(command) {
		return true, "reads a protected config/secret path"
	}

	return false, ""
}

// secretEnvRefRe matches a reference to a known secret-bearing environment
// variable in POSIX ($VAR, ${VAR}) or PowerShell ($env:VAR) form.
var secretEnvRefRe = regexp.MustCompile(
	`(?i)\$(?:\{|env:)?(?:WEBEX_BOT_TOKEN|WMCP_TOKEN|OPENAI_API_KEY|CHALLENGE)\b`,
)

// buildSecretReadRegex compiles a pattern matching any protected path literal
// appearing in a command string. Best-effort; used only for execute_command.
func buildSecretReadRegex(paths []string) *regexp.Regexp {
	if len(paths) == 0 {
		return nil
	}
	parts := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		parts = append(parts, regexp.QuoteMeta(p))
		// Also match forward-slash form of a Windows path in case the command
		// uses mixed separators.
		if alt := filepath.ToSlash(p); alt != p {
			parts = append(parts, regexp.QuoteMeta(alt))
		}
	}
	return regexp.MustCompile("(?i)(" + strings.Join(parts, "|") + ")")
}
