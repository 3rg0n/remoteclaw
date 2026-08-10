package security

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Category labels the kind of risk a rule detects. It exists so the rule table
// answers "what does RemoteClaw refuse?" by group rather than as a flat list of
// regexes.
type Category string

const (
	CategoryDestructive Category = "destructive"
	CategoryPrivilege   Category = "privilege"
	CategoryShellBypass Category = "shell-bypass"
	CategoryNetworkExec Category = "network-exec"
	CategorySecretRead  Category = "secret-read"
)

// Disposition is what happens when a rule matches.
type Disposition int

const (
	// DispositionChallenge blocks the command, but the operator may confirm it
	// via challenge-response and have it run.
	DispositionChallenge Disposition = iota
	// DispositionHard blocks the command with no remote confirmation path.
	// Reserved for config/secret access, which requires local administration
	// (ADR 0003) — a chat-delivered challenge response must not unlock the very
	// secrets the lockdown protects.
	DispositionHard
)

// Verdict describes a policy denial.
type Verdict struct {
	Category    Category
	Disposition Disposition
	Reason      string
}

// matcher reports whether a command matches a rule.
type matcher func(command string) bool

// rule is one entry in the policy table.
type rule struct {
	category    Category
	disposition Disposition
	reason      string
	match       matcher
}

// pattern builds a matcher from a case-insensitive regular expression. All
// rules are case-insensitive: `SUDO` and `sudo` are the same command, and
// Windows shells are case-insensitive throughout.
func pattern(expr string) matcher {
	return regexp.MustCompile("(?i)" + expr).MatchString
}

// cmdPos matches the position where a shell starts reading a command: the
// beginning of the line, or immediately after a separator (`;`, `&&`, `||`, `|`,
// newline), an opening subshell paren, a brace-group delimiter, or a `case`
// pattern's `)` — skipping any leading environment assignments, command
// wrappers, pipeline negation, and compound-statement keywords, which the shell
// allows before the command word without changing which word that is.
// `FOO=1 exec sh` and `nohup exec sh` are the same builtin as `exec sh`.
//
// It exists because several signals are only meaningful in command position.
// `exec` is a shell builtin there and a subcommand name anywhere else, so
// matching it unanchored refuses `docker exec` and `kubectl exec`; a bare `$(…)`
// there runs whatever the substitution prints, whereas `echo $(date)` merely
// interpolates it into an argument.
//
// The separator and prefix sets must cover every way a shell reaches a command
// word, not just the common ones: a set that omits `{` or `then` is evaded by
// `{ exec sh; }` and `if true; then exec sh; fi`, which is how this rule
// regressed once already. See TestPolicyBlocksExecAfterEveryCommandPosition.
//
// Removing the anchor to "harden" these rules is the wrong call, and ADR 0007
// records why — make TestPolicyAllowsNarrowedExecAndSubstitution fail first.
const cmdPos = `(?:^|[;&|\n({})]\s*)` + cmdPrefix

// cmdPrefix is the run of tokens a shell skips before the command word: any
// number of VAR=value assignments, wrappers that exec another command, the `!`
// pipeline negation, and the compound-statement keywords after which a command
// word follows (`if`, `elif`, `then`, `else`, `while`, `until`, `do`).
const cmdPrefix = `(?:(?:[A-Za-z_]\w*=\S*|env|nohup|nice|ionice|time|stdbuf|` +
	`setsid|command|builtin|xargs|if|elif|then|else|while|until|do|!)\s+)*`

// argEnd matches the end of a command argument: whitespace, end of string, or a
// shell metacharacter that terminates the word. Rules that pin an exact argument
// (`rm -rf /`, `chmod 777 /`) need it so the argument cannot be extended into
// something else — a bare `(\s|$)` misses the terminator forms, and
// `rm -rf /; echo done` then goes unmatched because the `/` is followed by `;`.
const argEnd = `(?:\s|[;&|)}<>]|$)`

// substOpen matches the opening of a `$(…)` command substitution. Also matches
// `$((` (arithmetic expansion, harmless) — accepted: over-matching arithmetic
// costs a confirmable prompt, under-matching a substitution costs the rule.
const substOpen = `\$\(`

// interpreter is the set of programs that treat an argument as code to run, for
// rules that care about a payload reaching one.
const interpreter = `(?:(?:ba|z|k|da)?sh|python[\d.]*|perl|ruby|node|pwsh|powershell)`

// evalFlag is the flag by which an interpreter takes code as an argument. The
// spelling varies by interpreter — `-c` for the shells and python, `-e` for
// perl/ruby/node, `-Command`/`-EncodedCommand` for PowerShell — and matching only
// `-c` would leave the others open.
const evalFlag = `-(?:c|e|p|-eval|command|encodedcommand)\b`

// riskyVerb is the set of commands whose *arguments* decide how much damage they
// do, so an argument computed at runtime defeats the literal-argument rules
// above: `rm -rf $(echo /)` never contains the `/` that the rm rules look for.
const riskyVerb = `\b(?:rm|rmdir|del|dd|mkfs\.\w+|shred|wipefs|chmod|chown|icacls|` +
	`kill|pkill|killall|shutdown|reboot|halt|insmod|modprobe|rmmod|crontab|` +
	`curl|wget|nc|ncat)\b`

// allOf builds a matcher that requires every sub-matcher to match, for rules
// whose signal is a combination rather than a single expression.
func allOf(ms ...matcher) matcher {
	return func(command string) bool {
		for _, m := range ms {
			if !m(command) {
				return false
			}
		}
		return true
	}
}

// CommandPolicyOptions configures which rule groups a policy carries.
// The two switches are independent because they come from independent config:
// security.dangerous_commands and security.lockdown.
type CommandPolicyOptions struct {
	// BlockDangerous enables the destructive / privilege / shell-bypass /
	// network-exec groups (security.dangerous_commands).
	BlockDangerous bool
	// BlockSecretReads enables the secret-read group (security.lockdown).
	BlockSecretReads bool
	// ProtectedPaths are canonicalized config/secret paths whose appearance in
	// a command string is itself a secret read. Only consulted when
	// BlockSecretReads is set.
	ProtectedPaths []string
}

// CommandPolicy is the single deny-list engine for execute_command. It replaces
// the former split between security.DangerousChecker and
// executor.Guard.IsSecretReadCommand: one rule table, one evaluation order, one
// denial shape. See ADR 0006.
//
// Matching is best-effort by nature — a process that can run arbitrary shell can
// evade any in-process pattern match (base64, indirect reads, copy-then-read).
// For secret reads the authoritative control is the OS file permission; for
// destructive commands it is the operator confirming via challenge-response.
// See ADR 0004 and THREAT_MODEL.md.
type CommandPolicy struct {
	rules []rule
}

// NewCommandPolicy builds the policy for the given options. A policy with both
// switches off blocks nothing; a nil *CommandPolicy is also safe to Check.
func NewCommandPolicy(opts CommandPolicyOptions) *CommandPolicy {
	p := &CommandPolicy{}
	if opts.BlockDangerous {
		p.rules = append(p.rules, dangerousRules()...)
	}
	if opts.BlockSecretReads {
		p.rules = append(p.rules, secretReadRules()...)
		if r, ok := protectedPathRule(opts.ProtectedPaths); ok {
			p.rules = append(p.rules, r)
		}
	}
	return p
}

// Check evaluates a command against the policy, returning nil when it is
// allowed.
//
// Hard denials are evaluated before confirmable ones so that a rule the
// operator may confirm cannot mask a rule they may not: `sudo cat <config>` is
// a secret read first and a privilege escalation second, and must not be
// offered as a challenge.
func (p *CommandPolicy) Check(command string) *Verdict {
	if p == nil {
		return nil
	}
	normalized := strings.TrimSpace(command)
	if v := p.checkDisposition(normalized, DispositionHard); v != nil {
		return v
	}
	return p.checkDisposition(normalized, DispositionChallenge)
}

// CheckHard evaluates only the rules that cannot be confirmed remotely. It is
// what a challenge-confirmed execution is re-checked against: confirmation
// authorizes a destructive command, never config/secret access.
func (p *CommandPolicy) CheckHard(command string) *Verdict {
	if p == nil {
		return nil
	}
	return p.checkDisposition(strings.TrimSpace(command), DispositionHard)
}

func (p *CommandPolicy) checkDisposition(normalized string, d Disposition) *Verdict {
	for _, r := range p.rules {
		if r.disposition != d {
			continue
		}
		if r.match(normalized) {
			v := Verdict{Category: r.category, Disposition: r.disposition, Reason: r.reason}
			return &v
		}
	}
	return nil
}

// dangerousRules are the destructive-command rules. Every one is confirmable:
// the operator who owns the machine is allowed to reformat it, so the control
// is proof of intent (challenge-response), not prohibition.
func dangerousRules() []rule {
	d := func(reason, expr string) rule {
		return rule{CategoryDestructive, DispositionChallenge, reason, pattern(expr)}
	}
	priv := func(reason, expr string) rule {
		return rule{CategoryPrivilege, DispositionChallenge, reason, pattern(expr)}
	}
	bypass := func(reason, expr string) rule {
		return rule{CategoryShellBypass, DispositionChallenge, reason, pattern(expr)}
	}
	// bypassM is bypass for rules whose signal is a combination of expressions
	// rather than one.
	bypassM := func(reason string, m matcher) rule {
		return rule{CategoryShellBypass, DispositionChallenge, reason, m}
	}
	net := func(reason, expr string) rule {
		return rule{CategoryNetworkExec, DispositionChallenge, reason, pattern(expr)}
	}

	return []rule{
		// Destructive filesystem operations — match flags in any order/combination.
		d("recursive deletion of root filesystem", `rm\s+(-\w*r\w*\s+)*(-\w*f\w*\s+)*/`+argEnd),
		d("recursive deletion of root filesystem", `rm\s+(-\w*f\w*\s+)*(-\w*r\w*\s+)*/`+argEnd),
		d("recursive deletion of root filesystem", `rm\s+-r\s+-f\s+/`+argEnd),
		d("recursive deletion of root filesystem", `rm\s+-f\s+-r\s+/`+argEnd),
		d("recursive deletion of drive root", `del\s+/s\s+/q\s+[A-Za-z]:\\`),
		d("formatting a drive", `format\s+[A-Za-z]:`),
		d("creating a filesystem (destructive)", `mkfs\.`),
		d("raw disk write via dd", `dd\s+.*of=/dev/`),
		d("secure file destruction via shred", `\bshred\b`),
		d("wiping filesystem signatures", `\bwipefs\b`),
		d("truncating file to zero bytes", `\btruncate\b.*--size\s+0`),
		d("fork bomb", `:\(\)\s*\{\s*:\|:\s*&\s*\}\s*;?\s*:`),

		// Dangerous permission changes.
		d("recursive world-writable permissions on root", `chmod\s+(-[a-zA-Z]*R[a-zA-Z]*\s+)?777\s+/`+argEnd),
		d("granting Everyone full access", `icacls\s+.*\s+/grant\s+Everyone:`),

		// System shutdown/reboot.
		d("system shutdown", `\bshutdown\b`),
		d("system reboot", `\breboot\b`),
		d("system halt", `\bhalt\b`),
		d("system halt via init", `\binit\s+0\b`),

		// Privilege escalation.
		priv("privilege escalation via sudo", `\bsudo\b`),
		priv("privilege escalation via doas", `\bdoas\b`),
		priv("privilege escalation via runas", `\brunas\b`),
		priv("privilege escalation via su", `\bsu\s+-`),
		priv("privilege escalation via pkexec", `\bpkexec\b`),

		// Shell evaluation/indirection (bypass attempts).
		bypass("shell eval (potential bypass)", `\beval\b`),
		// `exec` only replaces the shell when the shell is reading a command, so
		// it is matched in command position and as find's -exec action. Matching
		// it unanchored refused `docker exec` and `kubectl exec`, which are
		// ordinary container operations and the single most common false positive.
		bypass("shell exec (potential bypass)", cmdPos+`exec\b`),
		// find's -exec/-execdir run an arbitrary command per match, which the
		// unanchored \bexec\b rule used to cover incidentally.
		bypass("arbitrary command execution via find -exec", `\bfind\b.*\s-exec(?:dir)?\b`),
		bypass("sourcing script from absolute path", `\bsource\s+/`),
		// Command substitution is only a bypass where its *output* becomes a
		// command or an interpreter payload. `echo $(date)` interpolates a string
		// into an argument and is not.
		bypass("command substitution in command position", cmdPos+substOpen),
		bypass("command substitution passed to an interpreter",
			`\b`+interpreter+`\s+(?:-\S+\s+)*`+evalFlag+`\s*['"]?\s*`+substOpen),
		// A runtime-computed argument to a command whose arguments are the whole
		// risk. This is what keeps `rm -rf $(echo /)` blocked now that bare
		// substitution in argument position is allowed.
		bypassM("command substitution as an argument to a destructive command",
			allOf(pattern(riskyVerb), pattern(substOpen))),
		bypassM("backtick substitution as an argument to a destructive command",
			allOf(pattern(riskyVerb), pattern("`.+`"))),

		// Environment variable injection.
		bypass("LD_PRELOAD environment injection", `\bLD_PRELOAD\b`),
		bypass("LD_LIBRARY_PATH environment injection", `\bLD_LIBRARY_PATH\b`),

		// Kernel module loading.
		d("kernel module loading via insmod", `\binsmod\b`),
		d("kernel module loading via modprobe", `\bmodprobe\b`),
		d("kernel module removal via rmmod", `\brmmod\b`),

		// Scheduled execution.
		d("crontab modification", `\bcrontab\b`),
		d("at job scheduling", `\bat\s+`),

		// Container escape.
		d("privileged container execution", `docker\s+run\s+.*--privileged`),
		d("privileged container execution", `podman\s+run\s+.*--privileged`),

		// Piping remote content into an interpreter.
		net("piping remote content to shell", `curl\s+.*\|\s*(ba)?sh`),
		net("piping remote content to shell", `wget\s+.*\|\s*(ba)?sh`),
		net("piping remote content to interpreter", `curl\s+.*\|\s*python`),
		net("piping remote content to interpreter", `wget\s+.*\|\s*python`),

		// Reverse shells.
		net("potential reverse shell via /dev/tcp", `/dev/tcp/`),
		net("potential reverse shell via netcat", `\bnc\s+.*-[elp]`),
		net("potential reverse shell via ncat", `\bncat\b.*-[elp]`),
	}
}

// secretReadRules are the config/secret lockdown rules. All are hard denials:
// challenge-response proves the operator's intent over the chat channel, and
// the secrets at stake include the credentials for that channel.
func secretReadRules() []rule {
	s := func(reason string, m matcher) rule {
		return rule{CategorySecretRead, DispositionHard, reason, m}
	}

	const envDump = `\b(env|printenv|set)\b`
	// Substring, not word-bounded: "MYTOKENS" is as much a signal as "TOKEN".
	const secretWord = `(token|secret|challenge|key)`

	return []rule{
		s("reads the password store", pattern(`\bpass\s+(show|ls)\b|password-store`)),
		s("decrypts a secret with gpg", pattern(`\bgpg\s+(-d\b|--decrypt\b)`)),

		// A bare environment dump, and an environment dump filtered for
		// something secret-shaped.
		s("dumps environment variables that may contain secrets",
			pattern(`^(env|printenv|set)$`)),
		s("dumps environment variables that may contain secrets",
			allOf(pattern(envDump), pattern(secretWord))),

		s("reads the process environment", pattern(`/proc/self/environ|\$env:`)),
		s("references a secret environment variable",
			pattern(`\$(?:\{|env:)?(?:WEBEX_BOT_TOKEN|WMCP_TOKEN|OPENAI_API_KEY|CHALLENGE)\b`)),
	}
}

// protectedPathRule matches any protected path literal appearing in a command.
// Paths arrive already canonicalized (see executor.Guard, which owns
// canonicalization for the path-based checks).
func protectedPathRule(paths []string) (rule, bool) {
	parts := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		if p == "" {
			continue
		}
		parts = append(parts, regexp.QuoteMeta(p))
		// Also match the forward-slash form of a Windows path, in case the
		// command mixes separators.
		if alt := filepath.ToSlash(p); alt != p {
			parts = append(parts, regexp.QuoteMeta(alt))
		}
	}
	if len(parts) == 0 {
		return rule{}, false
	}
	return rule{
		category:    CategorySecretRead,
		disposition: DispositionHard,
		reason:      "reads a protected config/secret path",
		match:       pattern("(" + strings.Join(parts, "|") + ")"),
	}, true
}
