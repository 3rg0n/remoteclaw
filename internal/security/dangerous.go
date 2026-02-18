package security

import (
	"regexp"
	"strings"
)

// DangerousChecker detects dangerous commands that should be blocked before execution.
// Patterns are compiled once at construction for performance.
type DangerousChecker struct {
	patterns []dangerousPattern
}

type dangerousPattern struct {
	re     *regexp.Regexp
	reason string
}

// NewDangerousChecker creates a checker with a default set of dangerous command patterns.
func NewDangerousChecker() *DangerousChecker {
	dc := &DangerousChecker{}

	rules := []struct {
		pattern string
		reason  string
	}{
		// Destructive filesystem operations
		{`rm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?(-[a-zA-Z]*r[a-zA-Z]*\s+)?/(\s|$)`, "recursive deletion of root filesystem"},
		{`rm\s+(-[a-zA-Z]*r[a-zA-Z]*\s+)?(-[a-zA-Z]*f[a-zA-Z]*\s+)?/(\s|$)`, "recursive deletion of root filesystem"},
		{`del\s+/s\s+/q\s+[A-Za-z]:\\`, "recursive deletion of drive root"},
		{`format\s+[A-Za-z]:`, "formatting a drive"},
		{`mkfs\.`, "creating a filesystem (destructive)"},
		{`dd\s+if=.*\s+of=/dev/`, "raw disk write via dd"},

		// Fork bomb
		{`:\(\)\s*\{\s*:\|:\s*&\s*\}\s*;?\s*:`, "fork bomb"},

		// Dangerous permission changes
		{`chmod\s+(-[a-zA-Z]*R[a-zA-Z]*\s+)?777\s+/(\s|$)`, "recursive world-writable permissions on root"},
		{`icacls\s+.*\s+/grant\s+Everyone:`, "granting Everyone full access"},

		// System shutdown/reboot
		{`\bshutdown\b`, "system shutdown"},
		{`\breboot\b`, "system reboot"},
		{`\bhalt\b`, "system halt"},
		{`\binit\s+0\b`, "system halt via init"},

		// Privilege escalation
		{`\bsudo\b`, "privilege escalation via sudo"},
		{`\brunas\b`, "privilege escalation via runas"},
		{`\bsu\s+-`, "privilege escalation via su"},

		// Network exfiltration patterns (piping remote content to shell)
		{`curl\s+.*\|\s*(ba)?sh`, "piping remote content to shell"},
		{`wget\s+.*\|\s*(ba)?sh`, "piping remote content to shell"},
		{`curl\s+.*\|\s*python`, "piping remote content to interpreter"},
		{`wget\s+.*\|\s*python`, "piping remote content to interpreter"},
	}

	for _, r := range rules {
		dc.patterns = append(dc.patterns, dangerousPattern{
			re:     regexp.MustCompile(r.pattern),
			reason: r.reason,
		})
	}

	return dc
}

// Check tests whether a command matches any dangerous pattern.
// Returns (true, reason) if the command should be blocked, or (false, "") if safe.
func (dc *DangerousChecker) Check(command string) (blocked bool, reason string) {
	normalized := strings.TrimSpace(command)
	for _, p := range dc.patterns {
		if p.re.MatchString(normalized) {
			return true, p.reason
		}
	}
	return false, ""
}
