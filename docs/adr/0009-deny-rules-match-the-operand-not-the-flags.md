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
| `rm -rf /**` | **allowed** | **proceeds** |

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

The operand half was fragile in a third way, found while verifying the fix for
the first two: these commands take an operand *list* and act on every element of
it, so a rule that inspects only the first operand allows
`rm -rf backup /*` — valid shell, destroys the filesystem.

Two command positions also remained uncovered after ADR 0008 — a leading
redirection (`2>/dev/null exec sh`) and a backtick substitution
(``echo `exec sh` ``).

## Decision

**A rule about a destructive operation matches the operand it is destructive
toward, and treats the flags as noise.** Enumerating flags is a losing game;
enumerating the ways a shell can name the filesystem root is a bounded one.

- `operandRun` — `(?:(?:-{1,2}[\w-]*|[^\s;&|<>()][^\s;&|<>()]*)\s+)*` — the words
  a rule skips to reach the operand it cares about: any option words, and any
  *earlier operands*. The second half matters because these commands take a
  **list** and act on every element: `rm -rf backup /*` deletes the working copy
  and then everything in the root directory, and a rule that inspects only the
  first operand sees `backup` and allows it. Verified with coreutils that
  `rm -rf a b` removes both.
- `rootTarget` — `(?:["']?/["']?[*./"']*)` — the operand forms that mean the
  filesystem root: bare, globbed, dot forms, and quoted. The trailing class is a
  **run**, not one optional character, for the same reason flags are skipped
  rather than enumerated. The first version of this constant listed `\*` and
  `\.{1,2}` as single alternatives and therefore matched `/*` but not `/**`,
  `/***`, `/*/`, `/*/*`, `/.*`, `//*`, or `"/"**` — all of which expand to the
  contents of `/`. Quote characters are admitted at both ends and inside the run
  (`/"*"`, `'/'**`); a quote cannot begin a path segment, so this widens nothing
  real. The class deliberately contains **no path-segment characters**, which is
  what keeps `/var/log` from matching — it fails at the `v`.
- The four `rm` rules collapse into one (`\brm\s+` + `operandRun` + `rootTarget` +
  `argEnd`), and `chmod 777` gains the same shape. `chown` and `chgrp` on root are
  added, which the former rules did not cover in any form.
- `cmdPrefix` gains leading redirections (`\d*[<>]{1,2}\s*[^\s<>]\S*`) and
  `coproc`; `cmdPos` gains the backtick as a separator. The redirection
  alternative consumes its **target**, not just the operator: a target may be
  separated from the operator by whitespace, so `> out exec sh` is the same
  command as `>out exec sh`. A version stopping at the operator got this wrong in
  both directions at once — it left the target in command position, refusing
  `ls; > exec.log` (a truncate idiom that executes nothing) *and* missing
  `> out exec sh` (which reaches the builtin: verified, `exec` ran and replaced
  the shell). Excluding `<`/`>` from the target's first character keeps `{1,2}`
  from reading `>>` as one operator plus a target of `>`, which reintroduced the
  false positive for `ls; >> exec.log`.

Dispositions are unchanged: all of this stays `DispositionChallenge`. The
operator who owns the machine may wipe it — the control is proof of intent.

## Consequences

**Four rules became one and covered strictly more — verified differentially, not
argued.** "Strictly more" is the kind of claim that is easy to assert and easy to
get wrong, so both former patterns were reconstructed and compared against the
replacement over a mechanically generated corpus of 29,250 `rm`/`chmod`
invocations (flag words × target spellings × command-position prefixes ×
trailing separators × leading operands). **Zero inputs matched by the old rules
are allowed by the new one.** The collapse is also pinned by the pre-existing
table: reverting to the old `-r`-then-`-f` pattern fails
`TestPolicyBlocksDestructiveCommands/rm_separate_flags_reversed`, a case that
predates this change.

**The operand-list gap was found while verifying the collapse, not before it.**
The first version of this fix skipped flags only, and `rm -rf backup /*` — valid
shell, destroys the filesystem — was allowed. That is the same defect as the
original, one level out: the first version asked "what flags can precede the
operand" when it should have asked "which operand is the target." A rule about a
command that takes a list has to consider the whole list.

The widening this required is the largest in the file, so its allow half is the
load-bearing test: a multi-operand command naming any number of absolute paths
must stay allowed. That holds only because `rootTarget` is followed by `argEnd` —
the `/` of `/var/log/old` is followed by `v`, not a terminator, so an ordinary
absolute path cannot satisfy the target. Removing that `argEnd` turns this into a
rule that refuses every absolute path. That is not merely a comment: reverting it
turns 18 of the committed allow cases into false positives, so the invariant is
pinned by tests that fail loudly rather than by a note a refactor can ignore.

**The same mistake recurred a third time, in the same commit that fixed it
twice.** An independent review of the operand fix found `rm -rf /**` allowed.
`/**` expands to every entry in `/` exactly as `/*` does, but `rootTarget`
enumerated its glob and dot forms as single optional characters, so a doubled
star, a trailing slash after one, or a nested repetition slipped past — `/***`,
`/*/`, `/*/*`, `/.*`, `//*`, `"/"**` were all allowed. The first fix generalized
the *flags* to a run and left the *target* an enumeration; the recurrence is
what makes the point structural rather than anecdotal. The trailing class is now
a character run, and `TestPolicyBlocksGlobRunsOnRoot` is the enumeration of what
that run must swallow — kept as a table because the run is the kind of thing a
later narrowing would quietly undo.

**One position stays knowingly uncovered: a `case` branch.**
`case x in y) exec sh;; esac` reaches the builtin and is not matched. Covering it
requires treating a closing `)` as a separator, which matches the end of every
`$(…)` substitution and refuses `docker $(flags) exec web sh` — precisely the
trade [ADR 0007](0007-deny-rules-anchored-to-command-position.md) rejected and
ADR 0008 re-rejected for `}`. Recording it as accepted is better than a fix that
buys one obscure position at the cost of a common false positive. An operator
who writes a `case` statement to smuggle `exec` past a deny-list is well past
what an in-process pattern matcher is claimed to stop (ADR 0004).

**Reviewing the *shared* constants is what found the last defect, and it was
not in the changed rules.** Two independent review passes examined `operandRun`
and `rootTarget` — both confined to the four destructive rules — and neither
looked at `cmdPrefix`, which this ADR also widened and which feeds the `exec`
and `$(` rules instead. That is where the remaining bug was: the redirection
alternative consumed the operator but not its target. Checking every rule that
references a constant a change touches, rather than only the rules the change
was *about*, is the check that would have caught it first. It is now the
question this ADR asks of any future edit to `cmdPos`, `cmdPrefix`, `argEnd`,
`operandRun`, or `rootTarget`.

**Four new tables, and both directions controlled.**
`TestPolicyBlocksEveryRootTargetSpelling` carries 19 block and 10 allow cases;
`TestPolicyBlocksRootTargetAtAnyOperandPosition`, 12 block and 15 allow;
`TestPolicyBlocksGlobRunsOnRoot`, 21 block and 15 allow;
`TestPolicyBlocksExecAfterLeadingRedirectionAndBacktick`, 19 block and 19 allow.
The allow halves matter as much as the block halves, since widening an operand
pattern is one mistake away from refusing `rm -rf /tmp/build`.

Negative controls were run per-change rather than in aggregate, so each piece is
pinned by tests that fail without it and only on the intended inputs: reverting
the glob run in `rootTarget` fails exactly 21 subtests, all in the new table;
reverting `rootTarget` wholesale fails 12; the flag-run half of `operandRun`, 6;
the earlier-operand half, 14; the redirection prefix, 7; the redirection
*target*, 17 — 6 block cases and 11 allow, the split that showed the same
mistake was costing coverage and false positives simultaneously; the backtick,
1. Two
over-match probes (39 and 21 commands: absolute paths, multi-operand commands,
redirection in ordinary position, `coproc`/backticks in argument position,
routine sysadmin commands) produced no false positives, and the glob run adds a
third (35 commands, paths whose first segment is short or dot-like — `/tmp/.`,
`/var/*/tmp`, `/home/*/.cache`) with none either.

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
