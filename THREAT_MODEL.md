# MAESTRO Threat Model

**Project**: RemoteClaw — AI-powered remote system control via Webex
**Date**: 2026-07-13
**Framework**: MAESTRO (OWASP MAS + CSA) with ASI Threat Taxonomy
**Taxonomy**: T1-T15 core, T16-T47 extended, BV-1-BV-12 blindspot vectors

## Executive Summary

This revision re-models RemoteClaw after the inference-layer refactor (PR #1): the embedded **Ollama and AWS Bedrock SDKs were removed** and replaced with two transports behind the `ai.Converser` interface — **`inferd`** (local daemon over a Unix socket / Windows named pipe) and **`openai-compat`** (any OpenAI-compatible HTTP endpoint) — plus a new **`ai.mode: passthrough`** in which an inbound Webex message is executed directly as a command with no local inference.

Analysis across all 7 MAESTRO layers identified **19 verified findings**: 1 Critical, 7 High, 8 Medium, 3 Low. The dominant *new* risk is the **remote inference trust boundary** on RemoteClaw's **outbound** openai-compat client: `openai_base_url` is accepted with **no TLS/scheme enforcement**, so the API key and the full conversation can be sent in cleartext to — and crafted tool-calls received from — an operator-configured remote host (finding N-1). This is an *outbound client* exposure, not an inbound listener (see note below). The core pre-existing risk remains **unrestricted shell execution** gated only by regex + challenge-response.

> **Inbound surface (clarification).** RemoteClaw exposes **no inbound network listener for its function** — Webex is an outbound Mercury WebSocket + REST client, inferd is outbound local IPC, and openai-compat is an outbound HTTP client. The *only* inbound socket is the optional health endpoint (`internal/agent/health.go`), plaintext HTTP bound to `127.0.0.1:9090` by default and opt-out via `health.enabled`. Findings N-1/N-2 concern what RemoteClaw *sends outbound*, not anything it serves. The only inbound-surface finding is C-7 (health addr not hard-pinned to loopback).

The refactor **reduced** two risks: the old "no integrity check on Ollama auto-pull" is gone (inferd owns the model; RemoteClaw pulls nothing), and RemoteClaw no longer requires AWS credentials in its own environment. `govulncheck` reports **0 vulnerabilities affecting the code** (go-jose CVE GO-2026-4945 was patched in the same PR).

Three agentic risk factors are present: **Non-Determinism**, **Autonomy**, **Agent Identity**.

### Validation manifest

| Metric | Value |
|---|---|
| Layer agents run | L1, L2, L3, L4, L5, L6, L7, CVE, INTEGRITY |
| Agents grounded (read code / ran scanner) | L1, L2, L3, L4, L5, CVE, INTEGRITY |
| Agents discarded as ungrounded (0 tool calls, fabricated specifics) | **L6 (both attempts), L7** |
| Findings independently verified by orchestrator against source | 19 |
| Fabricated claims debunked and dropped | 4 (see below) |
| CVEs (govulncheck) | 0 affecting code |

**Fabricated claims dropped after source verification**: (1) "challenge TTL is 5 minutes / no brute-force lockout" — false; `challenge.go:19` sets `challengeTTL = 2 * time.Minute` and `:42` enforces `maxChallengeAttempts = 3`. (2) "passthrough strips user identity from the audit trail" — false; `handlePassthrough` logs `Email` and `SpaceID`. (3) "inferd uses gRPC on localhost:50051" — false; it uses a Unix socket / named pipe. (4) "ai.mode is not validated" — false; `config.go` `Validate()` rejects invalid modes. Findings that ungrounded agents gestured at but that a *grounded* agent independently confirmed by reading code (notably the `openai_base_url` TLS gap) are retained.

## Scope

- **Languages**: Go 1.26
- **AI Components**: Yes — pluggable inference via `inferd` (local daemon) or `openai-compat` (remote OpenAI-compatible HTTP); agentic tool-call loop with 7 tools; optional passthrough mode with no inference
- **Entry Points**: `cmd/remoteclaw/main.go` (CLI: run, install, uninstall, status, version)
- **External Services**: Webex (Mercury WebSocket + REST), WMCP relay (optional), inferd daemon (local IPC), openai-compat endpoint (operator-configured HTTP)
- **Agentic Risk Factors**:
  - **Non-Determinism**: model outputs vary; a compromised/remote model backend is now operator-selected
  - **Autonomy**: commands execute at machine speed with no mandatory human gate (except challenge-response on blocked commands)
  - **Agent Identity**: Webex bot token / WMCP token are the only identities; the remote openai-compat endpoint is trusted by configuration, not cryptographic proof

## Risk Summary

Findings prefixed **N-** are new or materially changed by the refactor; **C-** are carried forward from the prior model and re-verified against current code; **R-** are risks the refactor removed or reduced.

| # | ASI Threat ID | Layer | Title | Severity | L | I | Risk | Risk Factors | Traditional Framework |
|---|---------------|-------|-------|----------|---|---|------|--------------|----------------------|
| C-1 | T2, T11 | L3,L4 | Unrestricted Shell Execution (execute_command) | Critical | 3 | 3 | 9 | Autonomy | CWE-78, OWASP A03, STRIDE:E |
| N-1 | T9, T22, BV-11 | L1,L4,L6 | openai_base_url (outbound client) — No TLS/Scheme Enforcement (key + conversation in cleartext, MITM) | High | 2 | 3 | 6 | Agent Identity | CWE-319, OWASP A02, STRIDE:S |
| N-2 | T47, T13, T12 | L7 | Rogue/Compromised openai-compat Endpoint Drives Tool Calls (outbound trust) | High | 2 | 3 | 6 | Non-Det, Autonomy | CWE-345, LLM01, STRIDE:T |
| C-2 | T11 | L3,L6 | Dangerous Command Checker Bypass (Regex Evasion) | High | 3 | 2 | 6 | Non-Det | CWE-78, CWE-184, OWASP A03 |
| C-3 | T3 | L4,L6 | Runs as Root/SYSTEM Service — Implicit Privilege Escalation | High | 2 | 3 | 6 | Autonomy | CWE-250, CWE-269, STRIDE:E |
| C-4 | T23 | L5 | No Tamper Protection on Audit Logs | High | 2 | 3 | 6 | — | CWE-778, STRIDE:T |
| C-5 | T9 | L1,L7 | WMCP `ws://` Only Warns, Does Not Block (token cleartext) | High | 2 | 2 | 4 | Agent Identity | CWE-319, STRIDE:S |
| C-6 | T2, T3 | L3 | write_file / kill_process Skip the Dangerous Checker | High | 2 | 2 | 4 | Autonomy | CWE-862, STRIDE:E |
| N-3 | T22 | L2,L5 | Tool Inputs Not Scrubbed in Audit Log | Medium | 2 | 2 | 4 | — | CWE-532, OWASP A09, STRIDE:I |
| N-4 | T7 | L3 | Passthrough Removes LLM Soft-Filter (deterministic gates only) | Medium | 2 | 2 | 4 | Autonomy, Non-Det | ASI-only, LLM01 |
| N-5 | T3 | L4 | inferd_socket Path Override Unvalidated (local MITM of inference) | Medium | 1 | 2 | 2 | Agent Identity | CWE-426, CWE-59 |
| C-7 | T3, T43 | L4 | Health Endpoint Can Bind Non-Loopback (no auth) | Medium | 2 | 2 | 4 | — | CWE-345, STRIDE:I |
| C-8 | T13, T36 | L4,L7 | Release Artifacts Unsigned / Checksums Unattested | Medium | 2 | 2 | 4 | — | CWE-347, BV-3 |
| C-9 | T22 | L4 | Unrestricted os.ExpandEnv on Config Strings | Medium | 1 | 2 | 2 | — | CWE-95 |
| C-10 | T32 | L3 | No Per-Iteration Timeout / Backoff in Agentic Loop | Medium | 1 | 2 | 2 | Autonomy | CWE-400, STRIDE:D |
| C-11 | T5 | L2,L3 | Tool Input Not Schema-Validated (mitigated by dispatch allowlist) | Medium | 2 | 1 | 2 | Non-Det | CWE-20, LLM07 |
| C-12 | T3 | L4 | readFile Skips Sensitive-Path Check | Low | 2 | 1 | 2 | — | CWE-22 |
| N-6 | T44 | L5 | Passthrough Blocked Commands Not Flagged in Audit Schema | Low | 2 | 1 | 2 | — | CWE-778 |
| C-13 | T44 | L5 | No Backend/Provider Attribution in Audit Entries | Low | 1 | 1 | 1 | — | CWE-778 |

**Risk reduced by the refactor (tracked for completeness):**

| # | Former Threat | Change |
|---|---------------|--------|
| R-1 | T1 — No integrity check on Ollama model auto-pull (was HIGH) | **Removed.** inferd owns the model lifecycle; RemoteClaw performs no model pull. |
| R-2 | T22 — AWS credentials required in RemoteClaw's environment | **Removed.** Bedrock reached (if at all) via an openai-compat gateway; no AWS SDK, no AWS creds. |
| R-3 | Dependency CVE (GO-2026-4945, go-jose) | **Patched** to go-jose/go-jose/v4 v4.1.4 in the same PR. |
| R-4 | T22 — `read_file(".env")` exfil of tokens by a prompt-injected agent | **Mitigated.** Secrets move to `pass` (encrypted, no readable file), and the lockdown guard + OS file ownership deny the agent's tools access to config/`.env`/store (ADR 0003). Residual: a shell-capable agent may still call `pass show` unless OS enforcement (low-priv service account) is in place — which is why OS enforcement, not the in-process guard, is the real boundary. |
| R-5 | T3 — RemoteClaw runs as root/SYSTEM (was HIGH, C-3) | **Mitigated when lockdown+OS enforcement is used.** Installer can run the service as a dedicated low-privilege account (`install --user`), so config/secrets owned by root are unreadable by the agent. Still operator-dependent; documented. |

## Layer Analysis

### Layer 1: Foundation Model

The model source is now one of two operator choices, which reshapes L1 risk.

**N-1 — `openai_base_url` accepted without TLS/scheme enforcement (HIGH).** `internal/ai/openai.go:28-40` builds the client with `option.WithBaseURL(baseURL)` + `option.WithAPIKey(apiKey)`; `internal/config/config.go:239-240` validates only that the URL is *non-empty* when the provider is openai-compat. No code requires `https://`. If an operator configures a plaintext or attacker-influenced `http://` URL for a non-loopback host, the bearer `openai_api_key` (Authorization header) and the entire conversation (system prompt, tool definitions, user messages) travel in cleartext and the model's tool-calls can be MITM-modified. The shipped example (`config.example.yaml`) shows `http://localhost:11434/v1`, which normalizes `http://`. *Mitigation*: reject non-`https://` base URLs for non-loopback hosts in `Validate()`.

**T6 / BV-4 — Prompt injection, direct and indirect (HIGH, carried forward, mitigations verified present).** `internal/ai/processor.go` now wraps user input in `<user_input>…</user_input>` (`wrapUserInput`) and tool output in `<tool_output>…</tool_output>` with tag-defanging (`sanitizeToolOutput`, 32 KB cap), and `internal/ai/prompt.go:36-40` instructs the model to treat delimited content as data. These are genuine defense-in-depth improvements over the prior model, but remain **soft** (probabilistic) boundaries, not structural guarantees — a jailbreak or multi-turn context poisoning can still override them. Note passthrough mode has *no* prompt and therefore no prompt-injection surface (see N-4).

**R-1 — Model integrity (risk reduced).** The former Ollama auto-pull-without-verification finding is gone: `internal/ai/inferd.go` connects to a daemon that owns model management; RemoteClaw pulls nothing. Trust shifts to the local inferd daemon (assumed trusted; see N-5 for the socket-override caveat).

**T16 — Model inconsistency (LOW).** Behavior can diverge between an inferd-hosted model and an arbitrary openai-compat model (tokenization, tool-call grammar, safety alignment); temperature is capped at 0.3 (`config.go`) but a remote backend may ignore it.

### Layer 2: Data Operations

**N-3 — Tool inputs not scrubbed in audit log (MEDIUM).** `internal/logging/audit.go:150-151` writes `entry.ToolInputs` via `evt.Strs("tool_inputs", …)` with **no** `scrubSecrets()` pass, whereas `RawMessage` and `Response` *are* scrubbed at `:137-138`. Tool inputs are serialized in `internal/agent/agent.go` as `fmt.Sprintf("%s(%v)", ToolName, Input)`; an `execute_command` whose command line embeds a secret (e.g. `mysql -pHUNTER2`) lands in the audit log unredacted. Both the AI loop and passthrough populate this field. *Mitigation*: run each `ToolInputs` element through `scrubSecrets` before writing.

**C-11 — Tool input not schema-validated (MEDIUM, mitigated).** `inferd.go:190-200` and `openai.go:199-216` unmarshal tool arguments into `map[string]any` and, on error, log a warning and continue with an empty map — no validation that arguments match the tool schema. Impact is bounded because `executor.dispatch` (`executor.go:77-95`) is an effective allowlist: an unknown tool name returns `"unknown tool"` and never executes. Go's `encoding/json` is memory-safe, so classic deserialization RCE (CWE-502) does not apply, but unbounded input size is not explicitly capped before unmarshal.

### Layer 3: Agent Frameworks

**C-1 — Unrestricted shell execution (CRITICAL).** `execute_command` runs arbitrary shell (`internal/executor/command.go`); this is the product's core function and its core risk. Gated by the dangerous-command checker and challenge-response, but those are policy layers, not a sandbox.

**N-4 — Passthrough removes the LLM soft-filter (MEDIUM).** In `ai.mode: passthrough`, `messageHandler` routes the raw message to `handlePassthrough`, which executes it directly. **The guardrail wiring was verified intact**: passthrough calls the *same* `executeToolGuarded` helper as the AI loop (`agent.go`), so the dangerous checker, challenge-response (spaceID correctly threaded via `spaceIDKey`), rate limit, allowlist, and audit all apply identically — the Integrity audit and orchestrator review both confirm this is **not** a guardrail bypass. The residual risk is the loss of the model's *interpretive* soft-filter (which in interpret mode can decline semantically dangerous but pattern-clean requests). This is a documented, accepted trade (ADR 0002); the deterministic gates are the real controls. *Mitigation*: require allowlist + challenge-response whenever passthrough is enabled.

**C-6 — write_file / kill_process skip the dangerous checker (HIGH).** `executor.go:48` applies the dangerous-command checker **only** to `execute_command`. `write_file`, `kill_process`, and others dispatch unchecked (`write_file` has a sensitive-path check, but there is no unified capability gate). A model (or rogue endpoint, N-2) can chain `list_dir` → `write_file` (drop a script) → `execute_command` (a checker-clean invocation) to act. *Mitigation*: extend policy checks across all state-changing tools.

**C-10 — No per-iteration timeout/backoff (MEDIUM).** `processor.go` enforces a 5-minute whole-loop deadline and a 3-consecutive-error circuit breaker, but no per-iteration timeout or backoff; a backend that hangs near the per-tool timeout can consume the full window.

### Layer 4: Deployment Infrastructure

**C-3 — Runs as root/SYSTEM service (HIGH).** `internal/service/manager.go` installs an always-on system service; `agent.go` warns on root but does not refuse. A low-privilege Webex user's message is executed by a root/SYSTEM process — implicit privilege escalation with no capability dropping/seccomp. *Note the refactor's deployment tension*: inferd is a **per-user** daemon (stops at logout), while RemoteClaw is a system service — the socket path and lifecycle may not align, encouraging insecure workarounds.

**N-5 — inferd_socket path override unvalidated (MEDIUM).** `inferd.go` `dialInferdOverride` connects to whatever `ai.inferd_socket` path is configured, with no validation (no traversal/ownership check). A local attacker able to edit config or plant a socket could interpose a malicious daemon and control model output → tool calls. Local-only, so likelihood is low. *Mitigation*: restrict to platform-default directories; validate ownership.

**C-7 — Health endpoint can bind non-loopback (MEDIUM).** `health.addr` defaults to `127.0.0.1:9090` but is operator-settable to `0.0.0.0` with no auth, exposing uptime/last-message/connection status.

**C-8 — Release artifacts unsigned (MEDIUM).** `.github/workflows/release.yml` publishes binaries + checksums with no cryptographic signing; a compromised CI/account can forge both. Installers verify checksums but not signer identity.

**C-9 — Unrestricted `os.ExpandEnv` (MEDIUM).** `config.go` `expandEnvVars` expands `${VAR}` in several config strings (`openai_base_url`, `challenge`, `audit_log`, `inferd_socket`) from the process environment with no allowlist; in service mode the inherited environment is the injection surface.

### Layer 5: Evaluation & Observability

**C-4 — No tamper protection on audit logs (HIGH).** `audit.go` opens files `O_APPEND` at mode 0600, but there is no signing/HMAC/append-only enforcement; a privileged user can edit or delete entries (T23), and audit logging can be disabled entirely by config (only a startup warning fires).

**N-6 — Passthrough blocked commands not distinctly flagged (LOW).** A blocked passthrough command is audited (with `Email`/`SpaceID` — the "identity stripped" claim was false), but the schema has no explicit `Blocked` flag distinct from `Confirmed`, so blocked-vs-executed is inferred from response text. *Mitigation*: add an explicit `Blocked` field.

**C-13 — No provider attribution (LOW).** Audit entries do not record which backend (inferd vs openai-compat) served a request, hampering post-incident tracing if configuration changes.

### Layer 6: Security & Compliance

**N-1 (cross-listed)** — the openai-compat TLS gap is equally an L6 secrets/identity issue: the bearer key can transit cleartext.

**Challenge-response — verified sound.** `challenge.go` uses AES-256-GCM over a scrypt-derived key, keyed by `spaceID`, with `challengeTTL = 2 * time.Minute` and `maxChallengeAttempts = 3` brute-force lockout. (The ungrounded "5-minute TTL / no lockout" claims were false and dropped.)

**Allowlist — enforced before handling.** The email allowlist is applied in the connect layer (`native.go` `IsAllowedInRoom`) before `messageHandler` runs, strict in group rooms (empty list = deny all). Passthrough does not relax this.

**VCS hygiene (.gitignore audit).** Derived from `git ls-files` ∩ root `.gitignore` (authoritative recon):

| Check | Result |
|---|---|
| `.gitignore` exists at root | ✅ |
| Secrets: `.env`, `.env.*`, `config.yaml`/`.yml` ignored | ✅ (with intentional `!config/config.example.yaml`) |
| Secrets: `*.pem` / `*.key` / `*.p12` / `*.pfx` / `*.jks` / `credentials*` | ⚠️ **Missing** — a generated key/cert would not be ignored (T22, low: no such files are currently tracked) |
| Audit logs `*.jsonl` ignored | ✅ |
| Build artifacts (binaries, `dist/`, `bin/`, `vendor/`) | ✅ |
| IDE (`.idea/`, `.vscode/`, `*.swp`) | ✅ |
| Secret-bearing file tracked in git? | ❌ none — `config.example.yaml` contains only `${ENV}` placeholders (verified) |

*Mitigation (low)*: add `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`, `*.crt`, `credentials*`, `*secret*` to `.gitignore` as a defense-in-depth measure.

### Layer 7: Agent Ecosystem

**N-2 — Rogue/compromised openai-compat endpoint (HIGH).** With openai-compat, RemoteClaw treats an operator-configured remote host as the reasoning brain. A malicious or hijacked endpoint (or MITM enabled by N-1) can return crafted `tool_use` calls that drive local execution. **Bounded** by three downstream controls: `executor.dispatch` rejects unknown tool names, the dangerous-command checker gates `execute_command`, and challenge-response gates blocked commands. Residual risk: any checker-clean command the endpoint induces will run. *Mitigation*: restrict `openai_base_url` to organization-controlled endpoints; prefer local inferd for sensitive hosts; enforce TLS (N-1).

**Supply-chain governance (MEDIUM, informational).** `go.mod` pins `github.com/3rg0n/inferd/clients/go` to a **pseudo-version commit** (`v0.0.0-…-59d33b172d49`), not a signed release tag, because upstream publishes no `clients/go/` submodule tags (tracked as inferd issue #48). No SBOM or license-audit step in CI. *Mitigation*: pin to a signed release tag once available; add SBOM generation.

**C-5 — WMCP `ws://` only warns (HIGH).** `internal/connect/wmcp.go:43-45` logs a warning when the endpoint is not `wss://` but proceeds to dial, sending the WMCP auth token in cleartext. *Mitigation*: reject non-`wss://` endpoints.

## Agent/Skill Integrity

RemoteClaw's "agent definition" is its system prompt (`internal/ai/prompt.go`) plus the granted tool set (`internal/ai/tools.go`) and project instructions (`CLAUDE.md`). No `.claude/` agent YAML or MCP config exists. The Integrity audit (grounded, 11 tool calls) found **no misalignment**:

| File | Type | Declared Intent | Misalignment | ASI Threat | Severity | Observable |
|------|------|-----------------|--------------|------------|----------|------------|
| internal/ai/prompt.go + tools.go | System prompt + tools | "system administration agent" | None — sysadmin role ↔ sysadmin tools, capabilities explicitly disclosed (prompt lines 15-23), mandatory safety constraints stated (lines 25-34) | — (aligned) | Info | Yes |
| internal/agent/agent.go (passthrough) | Execution mode | Webex-as-SSH for a remote AI (ADR 0002) | Scope is disclosed and gated; not hidden (MA-4 considered, not upheld) | T7 (residual) | Low | Yes (audit-logged) |
| internal/service/manager.go | Service install | Install/uninstall service | None — standard kardianos/service, no hidden persistence | — | Info | Yes |

## Dependency CVEs

| Package | Version | CVE | CVSS | Fixed In | Reachable | Risk |
|---------|---------|-----|------|----------|-----------|------|
| _(none affecting code)_ | — | — | — | — | — | — |

*Scanned with: `govulncheck ./...` — 0 vulnerabilities in reachable code. `go-jose/go-jose/v4` was upgraded to v4.1.4 in PR #1, remediating GO-2026-4945 (the finding present in the prior model).*

## Recommended Mitigations (Priority Order)

1. **Enforce TLS on `openai_base_url` (N-1).** In `config.Validate()`, reject non-`https://` base URLs for non-loopback hosts; allow `http://` only for localhost. Closes cleartext key/conversation exposure and the easiest MITM path into tool execution.
2. **Constrain the remote-endpoint trust boundary (N-2).** Document that openai-compat trusts the endpoint as the reasoning brain; recommend org-controlled endpoints and local inferd for sensitive hosts. Keep the `dispatch` allowlist as the backstop.
3. **Run as a dedicated low-privilege user (C-3).** Ship install guidance/defaults for a non-root service account; consider seccomp/AppArmor (Linux) or a restricted service account (Windows).
4. **Scrub secrets from `ToolInputs` in the audit log (N-3).** Apply `scrubSecrets` to each element before writing.
5. **Extend the dangerous-command/policy gate beyond `execute_command` (C-6).** Apply capability checks to `write_file` and `kill_process`.
6. **Reject `ws://` for WMCP (C-5)** and add tamper-evidence to audit logs (C-4).
7. **Validate `inferd_socket` (N-5)**, restrict the health bind address to loopback (C-7), and sign release artifacts (C-8).
8. **Hardening backlog**: `.gitignore` key/cert patterns, provider attribution + explicit `Blocked` flag in audit (N-6, C-13), env-var expansion allowlist (C-9), per-iteration loop timeout (C-10).

## Trust Boundaries

1. **Webex user → RemoteClaw** — authenticated by bot token; authorized by email allowlist (strict in group rooms). Untrusted input crosses here.
2. **RemoteClaw → inference backend** — *new/changed*. For inferd: a local IPC socket (trusted daemon, but the socket path is an integrity boundary — N-5). For openai-compat: an outbound HTTP call to an operator-configured, cryptographically-unverified remote host — the **most significant new trust boundary** (N-1, N-2).
3. **Inference backend → executor** — model/endpoint output crosses into command execution; gated by the dispatch allowlist + dangerous checker + challenge-response. Passthrough removes the model from this edge but keeps the deterministic gates.
4. **RemoteClaw → operating system** — runs as a system service (often root/SYSTEM), executing shell commands (C-1, C-3).
5. **RemoteClaw ↔ WMCP relay** (optional) — token-authenticated; cleartext if `ws://` (C-5).

## Data Flow Diagram (Text)

```
                          TRUST BOUNDARY 1
   Webex user  ── message ──▶ [allowlist + rate limit] ──▶ messageHandler
   (untrusted)                                                   │
                                                 ┌───────────────┴───────────────┐
                                        interpret│                                │passthrough
                                                 ▼                                ▼
                                        AI Processor loop                 handlePassthrough
                                                 │                                │
                        TRUST BOUNDARY 2 (NEW)   │  Converse()                    │
                    ┌────────────────────────────┴───────────┐                    │
                    ▼                                         ▼                    │
            inferd daemon                          openai-compat endpoint          │
        (local UDS / named pipe)              (REMOTE HTTP, operator URL,          │
         [socket path = N-5]                   no TLS enforcement = N-1,           │
                    │                            rogue endpoint = N-2)             │
                    └────────────┬────────────────────────────┘                    │
                                 ▼  tool_use calls                                  │
                          TRUST BOUNDARY 3                                          │
                    [dispatch allowlist + dangerous checker + challenge-response]◀──┘
                                 │  (same executeToolGuarded for both paths)
                                 ▼
                          TRUST BOUNDARY 4
                    Executor ──▶ OS shell / filesystem / processes
                    (service often runs as root/SYSTEM = C-3, C-1)
                                 │
                                 ▼
                    Audit log (NDJSON) ── ToolInputs unscrubbed = N-3;
                                          no tamper protection = C-4
```
