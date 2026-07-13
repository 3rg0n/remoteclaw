# 0003. Secret storage via `pass`, and OS-enforced config/secret lockdown

- Status: accepted (airtight-guarantee aspiration amended by [ADR 0004](0004-privilege-separated-executor.md))
- Date: 2026-07-13

> **Note (added post-acceptance):** the body below refers in places to an
> "even as admin, the agent can't read secrets" guarantee via OS enforcement.
> [ADR 0004](0004-privilege-separated-executor.md) establishes that this is not
> achievable in-process given RemoteClaw runs with the installing user's
> privileges. Read this ADR's lockdown as **best-effort defense-in-depth**, per
> ADR 0004. The `pass` storage and in-process guard decisions here stand.

## Context

RemoteClaw holds service credentials (`WEBEX_BOT_TOKEN`, `WMCP_TOKEN`,
`OPENAI_API_KEY`) and runs an AI agent that can execute arbitrary shell
commands and read/write files on the host. Two distinct risks follow:

1. **Secrets at rest.** Storing tokens in a plaintext `.env` means any process
   (or a prompt-injected agent calling `read_file(".env")`) can read them.
2. **The agent attacking its own trust base.** Because the service historically
   ran as root/SYSTEM, a compromised or injected agent could read its own
   config/secrets or rewrite its own settings — disabling the very guardrails
   meant to contain it.

We already have a **challenge-response** system (ADR pre-dates this;
`internal/security/challenge.go`) that gates destructive commands on an
out-of-band passphrase. That defends a different thing (authorizing dangerous
actions) and must not be conflated with secret storage.

## Decision

**Two independent controls, plus an explicit opt-out.**

### 1. Native secret store (`pass`) with `.env` fallback
Service tokens are retrieved from [`pass`](https://www.passwordstore.org/)
(GPG-encrypted, `internal/secrets`) under the `remoteclaw/` prefix, via a strict
key allowlist (`webex_bot_token`, `wmcp_token`, `openai_api_key` — no arbitrary
store reads). A store value overrides the env/`.env` value. When `pass` is
unavailable (notably headless Linux with no unlocked keyring, or Windows), the
loader **falls back to `.env`/environment and logs a warning** — functionality
is preserved, security-at-rest is not, and the operator is told.

The **challenge passphrase is never stored** in `pass` or anywhere on the host.
Only the non-sensitive AES-GCM **ciphertext** lives on the machine (env value);
the passphrase stays in the operator's head and arrives via chat. This is
deliberate: since the agent can run shell as its own uid, any secret the process
can retrieve, the agent can retrieve — so the one secret that authorizes
destructive work must live *outside* the agent's reach.

### 2. Config/secret lockdown (`security.lockdown`, default `true`)
Two layers, secure by default:

- **OS-enforced (authoritative).** The installer creates a dedicated
  low-privilege service account and sets config/`.env`/store ownership to root
  (mode 600/700), **not readable by the service account**. The agent — even via
  `execute_command`, even though the host admin installed it — cannot read or
  modify its config/secrets, because the OS denies the service account the read.
  Changing settings requires local root/Administrator access.
- **In-process (defense-in-depth).** `internal/executor.Guard` hard-denies the
  file tools (`read_file`/`write_file`/`list_dir`) on protected paths
  (canonicalized to defeat symlink/relative bypass) and best-effort-denies
  obvious secret-reading `execute_command` invocations (`pass show`, `gpg -d`,
  env dumps, `$SECRET` references, direct reads of protected paths).

Operators who want the old wide-open behavior set `security.lockdown: false`.
The installer asks (default yes).

## Consequences

- **Easier / safer:** secrets encrypted at rest; the `read_file(".env")` exfil
  path is closed; with OS enforcement the "even as admin, the agent can't read
  its own secrets" property is *actually true*, and it also resolves the prior
  "runs as root" finding by moving the agent to a low-privilege account.
- **Harder / caveats:**
  - **The in-process command guard is not airtight.** A process running
    arbitrary shell can evade any in-process pattern match (base64, copy-then-
    read, indirection). It is explicitly defense-in-depth; the OS permission is
    the real boundary. We document this rather than imply the pattern list is
    complete.
  - **`pass` is per-user and Unix-first.** Headless Linux services and Windows
    fall back to `.env`. Aligning RemoteClaw to a per-user service (as inferd
    already is) makes the store reliably available — noted as follow-up.
  - **`pass` store vs. service-account model interact.** A root-owned config is
    unreadable by the service account by design; a GPG store under a user home
    needs that user's session. v1 documents this tension; the robust end state
    (a separate-uid secret broker) is left to a future ADR.
- The challenge system is unchanged and remains the control that actually
  resists a compromised agent, precisely because its secret is off-host.
