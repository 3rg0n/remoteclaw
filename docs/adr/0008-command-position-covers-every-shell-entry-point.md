# 0008. The command-position anchor must enumerate every shell entry point

- Status: accepted
- Date: 2026-08-10
- Amends: [0007](0007-deny-rules-anchored-to-command-position.md) (not superseded — the decision there stands; this corrects its implementation)

## Context

[ADR 0007](0007-deny-rules-anchored-to-command-position.md) anchored the `exec`
and `$(` rules to command position, and argued the anchor was safe because
`cmdPrefix` skips the assignments and wrappers a shell allows before the command
word. That argument was right about the *prefixes* and incomplete about the
*separators*. The shipped anchor was:

```
cmdPos    = (?:^|[;&|\n(]\s*) + cmdPrefix
cmdPrefix = (?:(?:VAR=val|env|nohup|nice|ionice|time|stdbuf|setsid|command|builtin|xargs)\s+)*
```

A review probe of shell constructs *not* in the committed test tables found the
separator set is not the full set of positions where a shell reads a command
word. These were allowed:

```
{ exec sh; }                       # brace group — `{` is a command-list opener
if true; then exec sh; fi          # a command word follows `then`
for i in 1; do exec sh; done       # …and `do`
while true; do exec sh; done
until false; do exec sh; done
! exec sh                          # pipeline negation
time { exec sh; }
```

Each runs the `exec` builtin. The rule that ADR 0007 says is anchored to command
position was, in these positions, not applied at all.

A second and independent defect surfaced in the same probe, in rules that pin an
exact argument:

```
rm\s+(-\w*r\w*\s+)*(-\w*f\w*\s+)*/(\s|$)
```

The trailing `(\s|$)` requires whitespace or end-of-string after the `/`, so a
shell metacharacter terminating the word evades it. `rm -rf /;` and
`rm -rf /; echo done` were **allowed**. This one is older than ADR 0007 — it came
over verbatim from the pre-consolidation `DangerousChecker`, where it had the same
hole. `rm -rf /` was blocked and `rm -rf /;` was not, for as long as the rule has
existed.

Both defects share a cause worth naming: **the tests enumerated the cases the
author thought of.** ADR 0007's tables are good tables — 22 allow cases, 32 block
cases, including six prefix evasions — and they all passed while `{ exec sh; }`
ran. Coverage of an adversarial surface is not evidence of completeness when the
adversary picks the input.

## Decision

**A positional anchor is only as good as its enumeration of shell entry points, so
enumerate them explicitly and test the enumeration, not just the rule.**

- `cmdPos` gains the brace-group opener as a separate alternative:
  `(?:^|[;&|\n(]\s*|\{\s+)`. The `{` counts **only when followed by
  whitespace**, and the closing `}` is not a command position at all. Both
  restrictions are load-bearing — see the brace-expansion consequence below.
- `cmdPrefix` gains the compound-statement keywords after which a command word
  follows (`if`, `elif`, `then`, `else`, `while`, `until`, `do`) and the `!`
  pipeline negation. These belong in the prefix set rather than the separator set
  because they are words the shell consumes before the command word, exactly like
  `nohup`.
- A new `argEnd` — `(?:\s|[;&|)}<>]|$)` — replaces the bare `(\s|$)` in every rule
  that pins an exact argument (the four `rm` root rules and `chmod 777 /`). An
  argument ends at whitespace, at end of string, or at a metacharacter that
  terminates the word.

Dispositions are unchanged: everything here stays `DispositionChallenge`.

## Consequences

**The false-positive guarantee is preserved, and that is the constraint that made
this fix non-trivial.** Widening an anchor is the move ADR 0007 explicitly warns
against, so the fix is only acceptable if ADR 0007's allow-table still passes.
It does — all 22 cases, including `docker exec`, `kubectl exec`, `echo $(date)`,
`echo $((1+2))`, and `grep ExecStart`. The keywords added to `cmdPrefix` are
followed by a command word by definition, so they cannot introduce a match in
argument position.

**Widening the anchor immediately re-broke what ADR 0007 protects, and the fix for
that is the most delicate part of this change.** The first attempt put `{` and `}`
in the separator set. `}` then matched the end of a `${VAR}` expansion, and the
policy refused:

```
docker ${FLAGS} exec web sh        kubectl ${OPTS} exec -it pod/api -- ls
echo ${A} $(date)                  cp file{a,b} exec
```

That is exactly the false-positive class ADR 0007 exists to prevent, produced by
the fix for the under-match. Both restrictions in the Decision are what resolve
it, and both are shell rules rather than heuristics:

- A closing `}` cannot be followed by a command word — `{ echo a; } echo b` is a
  syntax error — so a real command after a brace group always arrives via a
  separator already covered. `}` buys no coverage and costs the expansion match.
- A brace *group* requires whitespace after `{`; brace *expansion* never has it
  (`{echo a;}` is not a valid group). Requiring `\{\s+` separates
  `{ exec sh; }` from `--exclude={exec,build}` and `cp file{a,b}` exactly.

The general lesson: an anchor fix has two failure directions, and fixing the
under-match without re-testing the over-match trades one defect for the other.

**Three new committed tables pin the enumeration itself**, which is the durable
part of this change:

- `TestPolicyBlocksExecAfterEveryCommandPosition` — 16 cases covering brace
  groups, `if`/`then`/`else`/`elif`, `for`/`while`/`until` + `do`, `!` negation, a
  wrapper applied to a brace group, and substitution in those same positions.
- `TestPolicyBraceExpansionIsNotCommandPosition` — the counterweight: 10 allow
  cases covering `${VAR}` expansion before a signal word and brace expansion
  without whitespace. This is the table that fails if someone "simplifies" the
  anchor by folding `{`/`}` into the separator character class.
- `TestPolicyBlocksExactArgumentRulesBeforeTerminator` — 11 block cases for
  terminator forms after `rm -rf /` and `chmod 777 /`, plus 3 allow cases
  (`rm -rf /tmp/build`, `rm -rf /var/log/old;`, `chmod 777 /tmp/scratch`) so the
  terminator cannot be "fixed" by dropping the anchor on the path.

Verified by negative control in both directions: reverting the two under-match
fixes fails 24 subtests across the first and third tables; reverting to the
`[;&|\n({})]` separator set fails 6 subtests in the second and no others.

**The `rm -rf /;` hole was a live gap in a shipped release, not a regression from
this cycle.** It predates the consolidation. It is recorded here rather than
quietly patched because the CHANGELOG entry for ADR 0006 claims the merged engine
was "verified differentially against both former engines" — true, and it still
missed this, because a differential test can only prove the new engine matches the
old one's behavior. Both engines had the same hole. Differential testing does not
find inherited bugs, and that limitation should be visible next time it is cited
as evidence.

**This does not make the matcher a shell parser.** The enumeration is now
substantially more complete, but the position of ADR 0007 and ADR 0004 stands: this
is a best-effort layer, an operator who can run commands can evade any in-process
pattern match, and the authoritative controls remain OS file permissions for
secrets and operator confirmation for destruction. The lockdown was checked during
this review and is *not* affected by either defect — the hard secret-read rules are
deliberately unanchored, so `{ cat <config>; }` and `if true; then pass show …; fi`
were blocked throughout.
