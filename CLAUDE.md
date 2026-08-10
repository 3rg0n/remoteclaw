# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**RemoteClaw** — AI-powered remote system control via Webex. A user creates a Webex bot, runs RemoteClaw locally with the bot token, and messages the bot in natural language ("restart nginx", "check disk usage"). The AI engine interprets the request, runs commands on the local machine through a tool-call loop, and reports back. Essentially "remote hands" for sysadmin via chat.

Module path: `github.com/3rg0n/remoteclaw` · Go 1.26.

## Build & Test

```bash
go build ./cmd/remoteclaw/                                    # Build
go test -race -count=1 ./...                                  # Full test suite with race detector
golangci-lint run ./...                                       # Lint
go test -run TestIntegration_ChallengeResponse ./internal/agent/   # Single test
```

Run the agent: `./remoteclaw run --config config.yaml`. Other CLI verbs (cobra): `install` / `uninstall` / `status` (system-service lifecycle via kardianos/service), `version`.

## Request Lifecycle (the big picture)

A message flows through layers that are wired together in `agent.New` (`internal/agent/agent.go`) — read that constructor first; it's the composition root where every subsystem is assembled and injected.

1. **Connect layer** (`internal/connect/`) receives the message. `Mode` is an interface with two implementations selected by `cfg.Mode`: `NativeMode` (Mercury WebSocket receive + Webex REST send) and `WMCPMode` (relay WebSocket). The agent only ever talks to the `Mode` interface — never a concrete type.
2. **Allowlist** gates who may talk to the bot. Native mode strips the bot @mention in group rooms (resolved from `/people/me` at connect time). See "Allowlist semantics" below.
3. **`Agent.messageHandler`** runs the pipeline: rate-limit check → challenge-response check → look up conversation history → call the AI processor → audit-log → format → send reply.
4. **AI Processor** (`internal/ai/processor.go`) runs the agentic tool-call loop against the `Converser` interface (inferd or openai-compat).
5. **Executor** (`internal/executor/`) dispatches tool calls to the 7 tool handlers and actually touches the system.

### The AI agentic loop (`internal/ai/processor.go`)

`Processor.Process` loops up to `max_iterations` times: call the model → if it returned tool_use blocks, execute them and feed results back → repeat until the model returns plain text. Non-obvious safeguards baked into this loop:

- **Prompt-injection defense**: user input is wrapped in `<user_input>…</user_input>` delimiters (`wrapUserInput`); tool output is wrapped in `<tool_output>…</tool_output>` and any `<user_input>` tags inside it are defanged (`sanitizeToolOutput`).
- **Tool output cap**: individual tool results are truncated to 32 KB before going back to the model.
- **Circuit breaker**: 3 consecutive tool-error iterations aborts the loop with a safe message.
- **Hard deadline**: the entire `Process` call is bounded by a 5-minute context timeout, independent of per-command timeouts.

`Converser` is the seam for testing — mock it to drive the loop without a real model. Two backends implement it: `inferd` (`internal/ai/inferd.go`, local daemon over a Unix socket / named pipe) and `openai-compat` (`internal/ai/openai.go`, any OpenAI-compatible HTTP endpoint). See [ADR 0001](docs/adr/0001-inference-via-inferd-and-openai-compat.md).

## AI Provider Resolution

`Config.ResolveAIProvider()` decides the backend: an explicit `ai.provider` (`inferd` or `openai-compat`) wins; empty resolves to **`openai-compat` when `ai.openai_base_url` is set, otherwise `inferd`**. `ai.mode: passthrough` skips inference entirely (no `Converser` is built) — see the Passthrough section below. inferd owns its own model, so `ai.model` applies only to `openai-compat`. Temperature is capped at 0.3 in `Config.Validate` regardless of backend.

## Security Model

This is a remote-code-execution tool by design, so the security layers are the point, not an add-on. All live in `internal/security/`.

- **One command deny-list engine** (`commandpolicy.go`): `security.CommandPolicy` is the *only* pattern matcher applied to `execute_command`, evaluated once in `Executor.Execute` before dispatch. Every rule carries a **category** (`destructive` / `privilege` / `shell-bypass` / `network-exec` / `secret-read`) and a **disposition**: `DispositionChallenge` (the operator may confirm it) or `DispositionHard` (no remote confirmation — config/secret access requires local administration). Two independent rule groups map to two independent config switches: `security.dangerous_commands` → the first four categories, `security.lockdown` → `secret-read`. See [ADR 0006](docs/adr/0006-one-command-policy-with-tagged-rules.md).
  - **Hard denials are checked before confirmable ones**, so a command matching both groups (`sudo cat <config>`) is refused as the secret read. Inverting this is a lockdown bypass, not a cosmetic reordering.
  - The verdict travels as data on `ToolResult.Denial`, not as a message prefix. Don't reintroduce a decision that parses `Error` — that is exactly how the confirmable/hard split silently broke before.
  - `executor.Guard` keeps the *path*-based half of the lockdown (`IsProtectedPath`, canonicalizing via `Abs` + `EvalSymlinks` + `Rel`) for the file tools, and feeds `ProtectedPaths()` into the policy. Deliberately not folded into string matching.
  - **`cmdPos` anchors a rule to shell command position** — start of line, or after `;`/`&&`/`||`/`|`/`(`, skipping `VAR=value` assignments and wrappers (`env`, `nohup`, `nice`, …). Used by the `exec` and `$(` rules, where an unanchored match refuses `docker exec` and `echo $(date)`. The prefix-skipping is load-bearing, not cosmetic: without it the anchor is evaded by `FOO=1 exec sh`. When narrowing a rule, keep whatever coverage it held incidentally as an explicit rule (that is why `find -exec`, the interpreter-`evalFlag` rule, and the `riskyVerb`+substitution rule exist) and verify differentially against the old pattern — a narrowing that only runs the new tests proves nothing.
- **Challenge-response** (`challenge.go`): the confirmation mechanism for blocked commands. **Non-obvious design**: the `CHALLENGE` env var holds an *AES-256-GCM ciphertext* (a known sentinel encrypted with a scrypt-derived key), produced by `EncryptChallenge`. The user's reply is treated as the *passphrase/decryption key* — successful GCM decryption to the sentinel proves knowledge of the key. There is no plaintext challenge stored anywhere. Pending challenges are keyed by `spaceID`, expire after 2 minutes, and lock out after 3 failed attempts.
- **The blocked-command → challenge handoff** lives in the shared `executeToolGuarded` helper in `agent.go`, called by **both** the AI processor loop and the passthrough handler: when a `execute_command` is denied with `Disposition == DispositionChallenge` and challenge-response is enabled, it calls `challengeStore.SetPending(spaceID, …)` (spaceID is threaded through `context` via `spaceIDKey`) and returns a "reply to confirm" message. Hard denials skip this entirely and surface the refusal. The next incoming message is checked against the store in `messageHandler`; a correct response routes to `handleChallengeConfirmation`, which runs the command via `Executor.ForceExecuteCommand` — which **re-checks the hard rules** (`CheckHard`), so confirmation authorizes a destructive command but never config/secret access. Audit-logged with `Confirmed: true`. Because both request paths share this one guard, interpret and passthrough enforce guardrails identically.

## Request Modes (interpret vs. passthrough)

`ai.mode` selects how an inbound message is handled — see [ADR 0002](docs/adr/0002-passthrough-mode.md):

- **`interpret`** (default): the `Converser`-backed AI loop interprets the message and drives tools.
- **`passthrough`**: no inference; `messageHandler` runs the message directly as an `execute_command` via `handlePassthrough`. `agent.New` builds no processor (`a.processor == nil` is the mode signal). Every guardrail still applies because passthrough routes through the same `executeToolGuarded` path — rate limit and challenge-response are checked in `messageHandler` before it, and the command policy + challenge handoff run inside it.
- **Rate limiting** (`ratelimit.go`): per-space token bucket, `rate_limit_per_min` requests/min with a hardcoded burst of 3.
- **Temperature is capped at 0.3** in `Config.Validate` regardless of config — high temperature increases non-determinism that can intermittently bypass safety behavior.
- **Audit logging** (`internal/logging/audit.go`): NDJSON, date-stamped files, 30-day retention. A startup warning fires if dangerous-commands or challenge-response is enabled but audit logging is not.

## Configuration (`internal/config/`)

- Loaded via viper (YAML) + defaults in `applyDefaults`. Validation in `Config.Validate` enforces mode-specific required fields (`bot_token` for native; `endpoint`+`token` for wmcp).
- **`.env` handling**: `godotenv.Overload()` runs first, so `.env` values *take precedence over* system env vars (note: `Overload`, not `Load`).
- Env expansion: only the specific string fields listed in `expandEnvVars` support `${VAR}` — adding a new env-backed config field means adding it there too.

## Allowlist Semantics (easy to get wrong)

`allowed_emails` matching is case-insensitive and behaves differently by room type (`connect.Allowlist`):
- Empty list + **direct** message → allow anyone.
- Empty list + **anything else** → deny everyone (strict).
- Populated list → only listed emails, in any room type.

`IsAllowedInRoom` tests `roomType == "direct"` for the permissive branch rather than excluding `"group"`, and inverting that is a fail-open. The upstream Webex handler infers the room type from Mercury activity tags and returns `""` when they're absent or unrecognized, so an unknown type is reachable — treating it as direct would let an untagged group room with an empty allowlist authorize everyone in it.

**Authorization is a single choke point, not a Mode concern.** `Agent.authorize` is called at the top of `messageHandler` — before the rate limiter, challenge store, or executor — so every `connect.Mode` is gated identically and a new Mode cannot ship without authz. `Mode` implementations carry no allowlist; their contract is to report provenance honestly (`Email`, `RoomType`). `WMCPMode` therefore reports `RoomType: "group"`, the strict setting, because the relay envelope has no room-type field and a relay cannot prove a 1:1 space. A nil allowlist denies. See [ADR 0005](docs/adr/0005-single-authorization-choke-point.md).

## Conventions

- **One tool = one handler.** The 7 executor tools are declared in `internal/ai/tools.go` (`AllTools`, the schema sent to the model) and dispatched in `Executor.dispatch`. Adding a tool means touching both, plus a handler file in `internal/executor/`.
- Tool params arrive as `map[string]any`; use the `getXParam*` helpers in `executor.go` for typed extraction (JSON numbers arrive as `float64`).
- Structured logging is zerolog throughout; get the logger via `logging.Get()`.
- Conversation history is per-space (keyed by `SpaceID`), capped at 20 messages by `ConversationManager`.
- Graceful shutdown in `Agent.Run` closes the connection, health server, challenge store, rate limiter, and audit logger — background goroutines (challenge cleanup, rate-limiter GC) must be stopped via their `Close()`.

## Repo Practices

Per the project's working agreements, keep these updated alongside code changes: `CHANGELOG.md` (Keep a Changelog format) and ADRs in `docs/adr/` for cross-cutting or hard-to-reverse decisions (security posture, wire formats, foundational deps). The README is the user-facing setup guide (bot creation, install, full config reference) — mirror user-visible config/CLI changes there.
