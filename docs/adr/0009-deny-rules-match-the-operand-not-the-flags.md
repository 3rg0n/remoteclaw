# 0009. Deny rules match the operand, not the flags

- Status: accepted
- Date: 2026-08-11
- Amends: [0008](0008-command-position-covers-every-shell-entry-point.md) (not
  superseded — the position it takes stands; this extends it to the operand side
  and closes a hole older than either ADR)

## Context

[ADR 0008](0008-command-position-covers-every-shell-entry-point.md) fixed the
*position* half of the deny rules and, in its own words, warned that "the tests
enumerated the cases the author thought of." A retrospective review of the two
commits that implemented it — probing the compiled rules rather than reading them
— found the same class of defect on the *operand* half.

The root-deletion rules matched a bare `/` and nothing else:

```
rm\s+(-\w*r\w*\s+)*(-\w*f\w*\s+)*/(?:\s|[;&|)}<>]|$)     # ×2 flag orderings
rm\s+-r\s+-f\s+/…  rm\s+-f\s+-r\s+/…                     # ×2 more
chmod\s+(-[a-zA-Z]*R[a-zA-Z]*\s+)?777\s+/…
```

**The one spelling they matched is the one the operating system already refuses.**
GNU coreutils has had `--preserve-root` on by default since 6.4:

```
$ rm -rf /
rm: it is dangerous to operate recursively on '/'
rm: use --no-preserve-root to override this failsafe
$ echo $?
1
```

So the rule fired on a command that cannot do damage, while the forms that
actually destroy a system were allowed:

| command | policy (before) | coreutils |
|---|---|---|
| `rm -rf /` | blocked | refuses, exit 1 |
| `rm -rf /*` | **allowed** | **proceeds** |
| `rm -rf --no-preserve-root /` | **allowed** | **proceeds** |
| `chmod 777 /*` | **allowed** | proceeds |
| `rm -rf "/"` | **allowed** | refuses |
| `rm -r --verbose -f /` | **allowed** | valid, exit 0 |

`rm -rf /*` is the canonical spelling of this attack, and it is *not* the same
argument as `/`: the shell expands the glob into every entry in the root
directory, so `rm` never sees a `/` operand and no failsafe applies. Verified on
coreutils 8.32 — it walked into `/bin` and stopped only on a permission error.
`chmod` has no failsafe to invoke at all; its `--no-preserve-root` is the
documented default.

`git log -S` confirms no glob form has ever appeared in
`internal/security/`. This was a live gap in every released version, like the
`rm -rf /;` hole ADR 0008 recorded, and it is the more serious of the two.

The flag half was fragile for a related reason: four rules enumerated
`-r`-then-`-f` and `-f`-then-`-r`, so `rm -r --verbose -f /` and
`rm --recursive --force /` fell between the cases.

Two command positions also remained uncovered after ADR 0008 — a leading
redirection (`2>/dev/null exec sh`) and a backtick substitution
(``echo `exec sh` ``).

## Decision

**A rule about a destructive operation matches the operand it is destructive
toward, and treats the flags as noise.** Enumerating flags is a losing game;
enumerating the ways a shell can name the filesystem root is a bounded one.

- `flagRun` — `(?:-{1,2}[\w-]*\s+)*` — replaces every hand-enumerated flag
  sequence. Any options, any order, or none: `rm /` is as worth confirming as
  `rm -rf /`.
- `rootTarget` — `(?:/(?:\*|\.{1,2})?|"/"?\*?|'/'?\*?)` — enumerates the operand
  forms that mean the filesystem root: bare, globbed, dot forms, and quoted.
- The four `rm` rules collapse into one (`\brm\s+` + `flagRun` + `rootTarget`),
  and `chmod 777` gains the same shape. `chown` on root is added, which the
  former rules did not cover in any form.
- `cmdPrefix` gains leading redirections (`\d*[<>]{1,2}\S*`) and `coproc`;
  `cmdPos` gains the backtick as a separator.

Dispositions are unchanged: all of this stays `DispositionChallenge`. The
operator who owns the machine may wipe it — the control is proof of intent.

## Consequences

**Four rules became one and covered strictly more.** Enumerating flag orderings
is what made the old rules both verbose and evadable, and the collapse is
verified by the pre-existing table: reverting `flagRun` to the old
`-r`-then-`-f` pattern fails `TestPolicyBlocksDestructiveCommands/rm_separate_flags_reversed`,
a case that predates this change.

**One position stays knowingly uncovered: a `case` branch.**
`case x in y) exec sh;; esac` reaches the builtin and is not matched. Covering it
requires treating a closing `)` as a separator, which matches the end of every
`$(…)` substitution and refuses `docker $(flags) exec web sh` — precisely the
trade [ADR 0007](0007-deny-rules-anchored-to-command-position.md) rejected and
ADR 0008 re-rejected for `}`. Recording it as accepted is better than a fix that
buys one obscure position at the cost of a common false positive. An operator
who writes a `case` statement to smuggle `exec` past a deny-list is well past
what an in-process pattern matcher is claimed to stop (ADR 0004).

**Two new tables, and both directions controlled.**
`TestPolicyBlocksEveryRootTargetSpelling` carries 19 block cases and 10 allow
cases — the allow half matters as much, since widening an operand pattern is one
mistake away from refusing `rm -rf /tmp/build`.
`TestPolicyBlocksExecAfterLeadingRedirectionAndBacktick` carries 10 block and 6
allow. Negative controls were run per-change rather than in aggregate, so each
piece is pinned by tests that fail without it and only on the intended inputs:
reverting `rootTarget` fails 12 subtests; `flagRun`, 6; the redirection prefix,
7; the backtick, 1. A 39-command over-match probe (absolute paths, redirection in
ordinary position, `coproc`/backticks in argument position, routine sysadmin
commands) produced no false positives.

**The lesson ADR 0008 drew, restated with evidence for the operand side.** ADR
0008 said an anchor fix has two failure directions. This adds: **a rule can be
precisely wrong** — matching the exact variant the OS already blocks while
missing the ones it does not. Writing a deny rule means asking what the operating
system does with each spelling, not only what the regex does. Neither the
differential corpus (ADR 0006), nor the anchor tables (0007), nor the entry-point
tables (0008) caught this, because all three compared the matcher against itself
or against its predecessor. This one was found by running the commands.

**Still not a shell parser, and still best-effort.** ADR 0004's position is
unchanged: an operator who can run commands can evade any in-process pattern
match, and the authoritative controls are OS file permissions for secrets and
operator confirmation for destruction.
