# 0007. Deny rules that mean something only in command position are anchored there

- Status: accepted
- Date: 2026-08-10

## Context

[ADR 0006](0006-one-command-policy-with-tagged-rules.md) consolidated two
deny-list engines into `security.CommandPolicy` but carried the inherited rules
over unchanged. Two of them matched anywhere in the command string:

- `\bexec\b` — intended as the shell builtin that replaces the current process.
- `\$\(` — intended as command substitution whose output is executed.

Both are correct signals *in command position* and meaningless anywhere else, so
matching them unanchored refused a large class of routine, riskless commands:

```
docker exec -it web sh          kubectl exec -it pod/api -- ls
podman exec -it api sh          docker-compose exec db psql
echo $(date)                    ls -la $(pwd)
systemctl status $(hostname)    echo $((1+2))
grep ExecStart /lib/systemd/system/nginx.service
```

`exec` here is a *subcommand name* belonging to `docker`/`kubectl` — the shell
never sees a builtin. `$(date)` is interpolated into an argument; its output is
text, not a command. `$((1+2))` is arithmetic expansion, not substitution at all.
`ExecStart` merely contains the substring.

This matters more than the inconvenience suggests. Every one of these produced a
confirmation prompt, and each prompt asked the operator to confirm something they
could see was harmless. A guardrail that cries wolf on `docker exec` and
`echo $(date)` trains the operator to confirm without reading — which degrades
exactly the prompts that are real. False positives at this volume are a security
problem, not an ergonomics problem, because the challenge is only worth anything
if the answer is ever "no".

The naive fix — anchor to start-of-string — is trivially evadable. A shell reads a
command word after separators (`;`, `&&`, `||`, `|`, newline, `(`), and it also
skips a run of `VAR=value` assignments and wrapper commands (`env`, `nohup`,
`nice`, `command`, …) before that word without changing which word it is.
`FOO=1 exec sh` and `nohup exec sh` are the same builtin as `exec sh`.

The broad rules also held real coverage *incidentally*, which a narrowing would
silently drop: `find -exec` (runs a command per match), a substitution passed to
an interpreter's eval flag (`sh -c '$(curl …)'`), and a substitution computing an
argument to a command whose arguments are the entire risk (`rm -rf $(echo /)`).

## Decision

**Anchor a rule to command position when — and only when — the signal it matches
is meaningful only there. Where narrowing drops coverage the broad rule held by
accident, replace that coverage with an explicit rule rather than keeping the
broad match.**

Concretely, in `commandpolicy.go`:

- `cmdPos` matches start-of-line or a separator, then skips `cmdPrefix` — the run
  of assignments and wrappers a shell allows before the command word. Anchoring
  without that skip would be evadable by prefixing anything.
- `exec` and `$(` are matched as `cmdPos+…`.
- Four explicit rules carry the incidental coverage: `find -exec`/`-execdir`;
  substitution reaching an `interpreter` via any spelling of its `evalFlag`
  (`-c`, `-e`, `-Command`, `-EncodedCommand`, … — matching only `-c` would leave
  the others open); and substitution or backtick computing an argument to a
  `riskyVerb`.
- `$((` arithmetic expansion is knowingly still matched by `substOpen`.
  Over-matching arithmetic costs one confirmable prompt; under-matching a real
  substitution costs the rule. The asymmetry decides it.

All of these remain `DispositionChallenge` — confirmable. This ADR changes *what*
matches, never the disposition of a match.

**The rationale is recorded here because the code looks wrong to a security
reader.** `exec` not being blocked everywhere reads like a gap, and the obvious
"hardening" is to delete the anchor. That would restore the false-positive flood
and buy nothing: an attacker who can run `docker exec` can run `sh` directly,
since RemoteClaw runs as the installing user (ADR 0004). Anyone considering
re-broadening these rules should first make the two test tables below fail.

## Consequences

**The narrowing is pinned by tests in both directions**, which is what makes it
safe to keep. `TestPolicyAllowsNarrowedExecAndSubstitution` asserts 22 previously
refused commands are now allowed; `TestPolicyNarrowingKeepsExecutionBlocked`
asserts 32 execution paths are still blocked *and* still confirmable. The second
table is the load-bearing one: it includes six prefix evasions a plain anchor
would have missed (`FOO=1 exec sh`, `LC_ALL=C exec /bin/bash`, `env exec sh`,
`nohup exec sh`, `nice exec sh`, `ls; FOO=1 exec sh`), each of the seven
interpreter eval-flag spellings, and both substitution forms (`$(…)` and
backticks) against a risky verb. Deleting the anchor makes the first table fail;
deleting `cmdPrefix` makes the second fail.

**This is pattern matching, and pattern matching over shell syntax is
approximate.** Anchoring makes it *less* wrong in both directions here, but it is
not a parser and does not become one. The policy remains a best-effort layer
inside the posture ADR 0004 describes — it raises the cost of casual misuse and
catches the common accident. It is not a boundary, and this ADR does not claim
one.

**A new rule now carries a positional question.** Adding a deny rule means
deciding whether its signal is position-dependent, and using `cmdPos` if so. That
is a real judgment the previous table did not ask for. The alternative — every
rule matching everywhere — is what produced the false positives.

**Operator-visible, in the permissive direction.** Commands listed in the Context
section above stop prompting. An operator who had learned to expect a prompt for
`docker exec` will not get one; nothing that was executable becomes non-executable.
