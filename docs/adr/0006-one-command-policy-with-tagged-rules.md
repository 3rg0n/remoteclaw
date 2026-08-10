# 0006. One command deny-list engine, with tagged rules

- Status: accepted
- Date: 2026-08-09

## Context

Through v0.6.0 two independent pattern-matching engines inspected the same
`execute_command` string, in two packages, with two rule formats:

- `security.DangerousChecker` (`dangerous.go`) — ~50 pre-compiled regexes for
  destructive commands, privilege escalation, shell bypasses, and network-exec.
  A match returned `Command blocked: <reason>` with `ExitCode 1`.
- `executor.Guard.IsSecretReadCommand` (`guard.go`) — a separate ad-hoc chain of
  `strings.Contains` tests and three regexes for config/secret reads. A match
  returned `Command blocked by lockdown: <reason>`.

`Executor.Execute` ran them in sequence, dangerous first, returning on the first
block. The costs were the obvious ones: two audit surfaces, two places to add a
rule, two rule formats to reason about, and no single answer to "what does
RemoteClaw refuse?"

The split was also *behaviourally* inconsistent in ways nobody had chosen:

- The dangerous rules were **case-sensitive**; the secret-read rules lowercased
  the command first. `sudo rm -rf /` was blocked and `SUDO rm -rf /` was not.
- A dangerous block routed to challenge-response, because
  `executeToolGuarded` recognised the `"Command blocked:"` prefix. A lockdown
  block did not, because its prefix was `"Command blocked by lockdown:"`. The
  confirmable/non-confirmable distinction was real and load-bearing, but it was
  encoded in **message wording**, which no test asserted and any copy-edit could
  have silently inverted.

Auditing the two engines against each other turned up a live vulnerability that
the split had hidden. Because the dangerous checker ran first and returned early,
a command matching *both* rule sets was reported as the dangerous match and
offered as a challenge. And `ForceExecuteCommand` — which runs the command after
the operator confirms — re-checked **nothing**, despite a comment claiming it
"re-validates the command against the dangerous checker". So:

```
sudo -E printenv OPENAI_API_KEY
  → blocked as "privilege escalation via sudo"  (confirmable)
  → operator replies with the challenge passphrase
  → ForceExecuteCommand runs it with no lockdown check
  → the API key is dumped into the chat transcript
```

The same chain worked with `sudo cat <config>`, `eval cat <config>`, and
`sudo pass show remoteclaw/webex_bot_token`: any secret read prefixed with a
confirmable token bypassed the lockdown entirely. This was verified against the
pre-change code, not inferred — see Consequences.

Neither engine was wrong in isolation. The bypass existed *in the seam between
them*, which is the argument for not having a seam.

## Decision

**One engine: `security.CommandPolicy`, a single rule table where every rule
carries a category and a disposition.** `internal/executor` consumes it; the
security boundary stays in `internal/security`.

- **Categories** — `destructive`, `privilege`, `shell-bypass`, `network-exec`,
  `secret-read`. The tag rides on the denial, so a refusal says what *kind* of
  refusal it is rather than only why.
- **Dispositions** — `DispositionChallenge` (the operator may confirm via
  challenge-response) and `DispositionHard` (no remote confirmation path).
  Everything destructive is confirmable: the operator owns the machine and is
  allowed to reformat it, so the control is proof of intent, not prohibition.
  **Secret reads are hard denials**, because the challenge response travels over
  the chat channel whose own credentials the lockdown protects — confirming would
  let a leaked or coerced passphrase unlock exactly the secrets at stake.
- **Hard denials are evaluated before confirmable ones.** A command matching both
  groups is reported as the hard one. This is what closes the precedence half of
  the bypass: `sudo cat <config>` is a secret read first and a privilege
  escalation second.
- **`ForceExecuteCommand` re-checks the hard rules** (`CheckHard`). Confirmation
  authorises a destructive command; it never authorises config/secret access.
  This closes the other half. The old comment claiming re-validation is now true.
- **The disposition is data, not prose.** `ToolResult.Denial` carries the
  `*security.Verdict`, and `executeToolGuarded` branches on
  `Denial.Disposition`. No security decision depends on message wording any more.
- **All matching is case-insensitive**, resolving the inconsistency in favour of
  the stricter reading. `SUDO` is `sudo` to any shell that accepts it, and
  Windows shells are case-insensitive throughout.
- **Two independent switches, two rule groups.** `CommandPolicyOptions` mirrors
  the config: `BlockDangerous` from `security.dangerous_commands`,
  `BlockSecretReads` from `security.lockdown`. Merging the engines did not merge
  the operator's controls.

**`Guard.IsProtectedPath` stays as it is, and stays separate.** It is path-based,
canonicalises via `filepath.Abs` + `EvalSymlinks` + `Rel`, and empirically
blocked all seven evasion variants tested during the review. That is the control
carrying real weight for the file tools; folding it into string matching would
trade a structural check for a textual one. `Guard` keeps ownership of path
canonicalisation and exposes `ProtectedPaths()` to feed the policy's
protected-path rule — the policy matches path literals in a command string and
cannot canonicalise on its own.

## Consequences

**A security fix ships with the refactor.** The confirm-to-bypass-lockdown chain
is closed at both ends (precedence, and the `CheckHard` re-check). It is covered
by `TestForceExecuteCommandStillDeniesSecretReads`, which asserts all four
variants, and by `TestPassthroughSecretReadGetsNoChallengePrompt` at the agent
level — a hard denial must produce no confirmation prompt and register no pending
challenge.

**The consolidation was verified differentially, not by inspection.** Both
pre-change implementations were extracted from git into a standalone harness and
run against the new policy over a 146-command corpus (every case from both old
test suites, plus benign sysadmin commands, case variants, and near-miss
secret-shaped strings). Result: **zero commands blocked by the old engines and
allowed by the new one**, and zero disposition mismatches. Six commands are newly
blocked, all of them the intended case-insensitivity fix (`SUDO rm x`, `REBOOT`,
`FORMAT C:`, `MKFS.ext4 …`, and two more). The old sequential `Execute` ordering
was then modelled explicitly to expose the precedence change, which is how the
bypass surfaced; that harness confirmed all four exploit variants against the
original code before being discarded. Porting test tables would only have proven
the tables agreed.

**Slightly more work per denied command.** Hard rules are scanned before
confirmable ones, so a destructive command is matched against the secret-read
group first. Both groups are pre-compiled regexes over a short string, on a path
that already ends in a process spawn.

**One place to add a rule, one shape to audit.** A new rule is a row in the table
with a category and a disposition; there is no second engine to remember. Every
command denial produces the same `ToolResult` shape regardless of category, so
the audit log no longer has two block formats to review.

**Public API changed within `internal/`.**
`Executor.SetDangerousChecker(*security.DangerousChecker)` becomes
`Executor.SetCommandPolicy(*security.CommandPolicy)`;
`security.DangerousChecker` and `Guard.IsSecretReadCommand` are gone;
`Guard.ProtectedPaths()` is new. All callers are in this repo.

**Operator-visible behaviour changes in two ways**, both intended: commands that
differ only in case from a blocked command are now blocked, and a secret read
that used to be offered as a challenge is now refused outright. The second is the
vulnerability fix; an operator who was relying on confirming their way to a
`printenv` was relying on the bug.
