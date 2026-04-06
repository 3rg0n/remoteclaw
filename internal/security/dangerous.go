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
		// Destructive filesystem operations — match flags in any order/combination
		{`rm\s+(-\w*r\w*\s+)*(-\w*f\w*\s+)*/(\s|$)`, "recursive deletion of root filesystem"},
		{`rm\s+(-\w*f\w*\s+)*(-\w*r\w*\s+)*/(\s|$)`, "recursive deletion of root filesystem"},
		{`rm\s+-r\s+-f\s+/(\s|$)`, "recursive deletion of root filesystem"},
		{`rm\s+-f\s+-r\s+/(\s|$)`, "recursive deletion of root filesystem"},
		{`del\s+/s\s+/q\s+[A-Za-z]:\\`, "recursive deletion of drive root"},
		{`format\s+[A-Za-z]:`, "formatting a drive"},
		{`mkfs\.`, "creating a filesystem (destructive)"},
		{`dd\s+.*of=/dev/`, "raw disk write via dd"},

		// Additional destructive tools
		{`\bshred\b`, "secure file destruction via shred"},
		{`\bwipefs\b`, "wiping filesystem signatures"},
		{`\btruncate\b.*--size\s+0`, "truncating file to zero bytes"},

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
		{`\bdoas\b`, "privilege escalation via doas"},
		{`\brunas\b`, "privilege escalation via runas"},
		{`\bsu\s+-`, "privilege escalation via su"},
		{`\bpkexec\b`, "privilege escalation via pkexec"},

		// Shell evaluation/indirection (bypass attempts)
		{`\beval\b`, "shell eval (potential bypass)"},
		{`\bexec\b`, "shell exec (potential bypass)"},
		{`\bsource\s+/`, "sourcing script from absolute path"},

		// Command substitution and shell metacharacter abuse
		{`\$\(`, "command substitution ($(...))"},
		{"`.+`", "backtick command substitution"},

		// Environment variable injection
		{`\bLD_PRELOAD\b`, "LD_PRELOAD environment injection"},
		{`\bLD_LIBRARY_PATH\b`, "LD_LIBRARY_PATH environment injection"},

		// Kernel module loading
		{`\binsmod\b`, "kernel module loading via insmod"},
		{`\bmodprobe\b`, "kernel module loading via modprobe"},
		{`\brmmod\b`, "kernel module removal via rmmod"},

		// Scheduled execution
		{`\bcrontab\b`, "crontab modification"},
		{`\bat\s+`, "at job scheduling"},

		// Container escape
		{`docker\s+run\s+.*--privileged`, "privileged container execution"},
		{`podman\s+run\s+.*--privileged`, "privileged container execution"},

		// Network exfiltration patterns (piping remote content to shell)
		{`curl\s+.*\|\s*(ba)?sh`, "piping remote content to shell"},
		{`wget\s+.*\|\s*(ba)?sh`, "piping remote content to shell"},
		{`curl\s+.*\|\s*python`, "piping remote content to interpreter"},
		{`wget\s+.*\|\s*python`, "piping remote content to interpreter"},

		// Reverse shell patterns
		{`/dev/tcp/`, "potential reverse shell via /dev/tcp"},
		{`\bnc\s+.*-[elp]`, "potential reverse shell via netcat"},
		{`\bncat\b.*-[elp]`, "potential reverse shell via ncat"},
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
