# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **CI enforces the full quality gate: `gofmt`, `go vet`, `golangci-lint`, and
  the race test.** Only the race test ran before, and only on a `v*` tag push —
  so formatting and lint were manual conventions, and the drift this release
  cleans up (see below) reached `master` unremarked. The gate now lives in one
  reusable workflow (`.github/workflows/ci.yml`) that runs on push and pull
  request and is *called* by `release.yml`, so the release path cannot run a
  staler gate than a PR does. Steps are ordered cheapest-first. The format check
  tests for empty output rather than an exit code, because `gofmt -l` exits 0
  even when it names files — the trap that let a bare `gofmt -l` pass as a gate.
  `golangci-lint` is pinned to v2.12.2 (matching `.golangci.yml`'s `version: "2"`)
  rather than tracking `latest`, so a new upstream release cannot fail a build
  with no commit to this repo.

### Security
- **`webex.allowed_emails` is now enforced in `wmcp` mode.** Through v0.6.0 the
  allowlist was implemented inside `NativeMode` only, so in `wmcp` mode the
  config field was silently ignored and any sender the relay forwarded could run
  commands. Authorization has been lifted into a single choke point,
  `Agent.authorize`, called at the top of `messageHandler` before the rate
  limiter, challenge store, or executor — so every connection mode is gated
  identically and a future `Mode` cannot ship without it. Fails closed (a nil
  allowlist denies); denials are logged and audited
  ([ADR 0005](docs/adr/0005-single-authorization-choke-point.md)).
  **Breaking for `wmcp` operators**: relayed senders are held to strict
  group-room semantics because a relay cannot prove a 1:1 space, so an empty
  `allowed_emails` now denies everyone instead of allowing everyone. List the
  emails you want to authorize. If you ran `wmcp` mode on ≤ v0.6.0, review your
  audit log.
- **Fixed a fail-open in the allowlist for spaces whose room type could not be
  determined.** `IsAllowedInRoom` took the permissive 1:1 branch for any room type
  that was not exactly `"group"`, and the upstream Webex handler infers the type
  from Mercury activity tags and returns `""` when they are absent or
  unrecognized. So a group room whose activity arrived untagged, with an empty
  `allowed_emails`, authorized every sender in it — the documented "empty list +
  group room → deny everyone" rule silently did not hold. The permissive branch
  now requires a positive `"direct"`, and everything else — `"group"`, an
  unrecognized value, or an empty one — requires an explicit allowlist entry. This
  predates the authorization rework, but that rework made this function the single
  gate for every connection mode, so its edge cases now matter everywhere.
- **`rm -rf /;` was not blocked.** The four `rm` root-deletion rules and the
  `chmod 777 /` rule required whitespace or end-of-string after the `/`, so any
  shell metacharacter terminating the argument evaded them: `rm -rf /;` and
  `rm -rf /; echo done` ran with no challenge prompt. This is **older than this
  release** — the pattern came over verbatim from the pre-consolidation
  `DangerousChecker` and had the same hole there, so it was a live gap in every
  prior version. Rules that pin an exact argument now end it with `argEnd`
  (whitespace, end of string, or a word-terminating metacharacter). Note that the
  differential verification cited below could not have caught this: both engines
  shared the hole, and a differential test only proves the new engine matches the
  old one ([ADR 0008](docs/adr/0008-command-position-covers-every-shell-entry-point.md)).
- **The command-position anchor missed brace groups and compound statements**, so
  `{ exec sh; }`, `if true; then exec sh; fi`, `for i in 1; do exec sh; done`,
  `while true; do exec sh; done`, and `! exec sh` reached the `exec` builtin
  unblocked — the rule the anchor was supposed to place, not applied. The separator
  set gains `{`/`}` and the prefix set gains `if`/`elif`/`then`/`else`/`while`/
  `until`/`do` and `!`. All 22 false-positive cases from
  [ADR 0007](docs/adr/0007-deny-rules-anchored-to-command-position.md) still pass —
  `docker exec` and `echo $(date)` are still allowed — and two new tables pin the
  enumeration of shell entry points itself
  ([ADR 0008](docs/adr/0008-command-position-covers-every-shell-entry-point.md)).
  Introduced by the narrowing earlier in this same release; never shipped.
- **Fixed a lockdown bypass via challenge-response confirmation.** A secret-read
  command prefixed with a confirmable token — `sudo -E printenv OPENAI_API_KEY`,
  `sudo cat <config>`, `eval cat <config>`, `sudo pass show …` — was reported as
  the *dangerous* match (the dangerous checker ran first and returned early), so
  it was offered as a challenge; `ForceExecuteCommand` then ran the confirmed
  command with no lockdown check at all, despite a comment claiming it
  re-validated. Confirming therefore dumped the secret into the chat transcript.
  Both halves are closed: hard denials now take precedence over confirmable ones,
  and a confirmed command is re-checked against the hard rules
  ([ADR 0006](docs/adr/0006-one-command-policy-with-tagged-rules.md)).
- Config/secret reads are **hard denials** with no challenge-response path. The
  challenge reply travels over the same chat channel whose credentials the
  lockdown protects, so confirming would let a leaked passphrase unlock exactly
  the secrets at stake. Destructive commands remain confirmable.
- Command matching is now case-insensitive throughout. The destructive rules were
  case-sensitive while the secret-read rules lowercased first, so `sudo rm -rf /`
  was blocked and `SUDO rm -rf /` was not.
- `health.addr` is validated as loopback-only at config load. The health endpoint
  is unauthenticated, so `0.0.0.0:9090`, `:9090`, a LAN address, or a hostname
  other than `localhost` is now rejected at startup with a message naming the
  fix. Override with `health.allow_non_loopback: true`, which logs a warning.

### Changed
- **One command deny-list engine.** `security.DangerousChecker` and
  `executor.Guard.IsSecretReadCommand` are replaced by `security.CommandPolicy`:
  a single rule table where each rule carries a **category** (`destructive`,
  `privilege`, `shell-bypass`, `network-exec`, `secret-read`) and a
  **disposition** (block-with-challenge or block-hard). One evaluation point, one
  ordering, one denial shape to audit, and one place to add a rule
  ([ADR 0006](docs/adr/0006-one-command-policy-with-tagged-rules.md)). Verified
  differentially against both former engines over a 146-command corpus: no
  command they blocked is now allowed. `Guard.IsProtectedPath` is unchanged and
  still separate — it is a canonicalizing path check, not string matching.
- The confirmable/non-confirmable distinction is carried as data
  (`ToolResult.Denial.Disposition`) instead of being inferred from the block
  message's prefix. `Executor.SetDangerousChecker` becomes
  `Executor.SetCommandPolicy`.
- `connect.Mode` implementations no longer perform authorization; their contract
  is to report provenance (`Email`, `RoomType`) accurately. `NewNativeMode` no
  longer takes `allowedEmails`. `WMCPMode` reports `RoomType: "group"` — the
  strict setting — since the relay envelope carries no room-type field.
- `ConversationManager` uses a plain `sync.Mutex` instead of an `RWMutex`:
  `GetHistory` refreshes the entry's TTL, so there is no read-only path and the
  former `RLock`-then-`Lock` sequence was an unnecessary lock-upgrade hazard.
- `WMCPMode.conn` is published via `atomic.Pointer` instead of being guarded by a
  mutex. The connection is replaced wholesale on reconnect while the read and
  heartbeat loops run; `websocket.Conn`'s methods are safe for concurrent use
  (except `Read`, which only ever runs on the read loop), so holding a lock
  across a blocking `Write` only serialized heartbeats behind responses.
- `getUsername()` resolves from the OS only. The `$USER`/`$USERNAME` fallbacks
  were caller-controlled and unset or wrong in exactly the contexts RemoteClaw
  runs in (systemd unit, LaunchAgent, Windows scheduled task).
- **Line endings are normalized to LF via `.gitattributes`,** and the ten Go
  files that had genuinely drifted are `gofmt`-clean. No behavior change; the
  reason it is worth an entry is that the drift was *invisible*: with
  `core.autocrlf=true` and no `.gitattributes`, a CRLF working tree against LF
  storage made `gofmt -l` report 32 of 56 files, 22 of them pure line-ending
  artifacts — so the 10 real ones were indistinguishable from noise and every
  `gofmt` diff was unreadable. `*.ps1` stays CRLF, which is what PowerShell
  expects. Contributors on Windows should re-materialize their working tree
  (`git rm --cached -r . && git reset --hard`) to pick up the new attributes;
  `git add --renormalize` is a no-op here, since the CRLF was only ever in the
  working tree and never in storage.

### Fixed
- **Tool calls now honor context cancellation.** Every executor handler took a
  `context.Context` and none of them read it, so the signature promised
  cancellability the bodies did not deliver: neither the processor's 5-minute hard
  deadline nor a per-command timeout could stop a tool call, and an abandoned
  request still wrote files and killed processes. `Executor.Execute` (and
  `ForceExecuteCommand`, which bypasses it) now refuses a cancelled or expired
  context before dispatch, so no tool has a side effect nobody is waiting for. The
  two handlers that can block mid-call check as they go: `read_file` reads in 32 KB
  chunks instead of one uninterruptible `io.ReadAll` over the whole 1 MB cap — the
  case that mattered, since a slow-backed file (network mount, `/dev/*`, a fifo)
  outlived the caller's deadline — and the recursive `list_dir` walk checks per
  entry and reports the abort rather than a directory error. Handlers with no
  interior blocking point (`write_file`, `kill_process`, `system_info`) name the
  parameter `_` so the reader can see the omission is deliberate.
- **`docker exec`, `kubectl exec`, and `echo $(date)` are no longer blocked.** The
  `exec` and `$(` rules matched anywhere in the command string, so any subcommand
  named `exec` and any command substitution was refused — the two most common
  false positives, and the kind that trains an operator to confirm blindly. Both
  are now anchored to *command position* (start of line, or after `;`/`&&`/`||`/
  `|`/`(`, skipping `VAR=value` assignments and wrappers like `nohup`), which is
  the only position where `exec` is the shell builtin and where a substitution's
  output becomes the command. Coverage the broad patterns held incidentally is now
  explicit: `find -exec`/`-execdir`, a substitution passed to an interpreter's
  eval flag (`-c`, `-e`, `-Command`, …), and a substitution or backtick computing
  an argument to a command whose arguments are the whole risk (`rm -rf $(echo /)`).
  Pinned by two committed tables in `commandpolicy_test.go`: 22 commands that the
  broad rules refused and must now be allowed, and 32 execution paths that must
  still be blocked — including six prefix evasions a plain anchor would have
  missed (`FOO=1 exec sh`, `nohup exec sh`, `env exec sh`)
  ([ADR 0007](docs/adr/0007-deny-rules-anchored-to-command-position.md)).
- Secret-read matching no longer compiles regexps on every call. `Guard.IsSecretReadCommand`
  rebuilt its env-dump pattern per invocation; the rules are now pre-compiled once
  when the command policy is constructed.

### Removed
- Dead exported API with no production callers: `Allowlist.Reload` (which made
  the allowlist immutable after construction, so its mutex is gone and reads are
  lock-free), `ConversationManager.Clear`/`ClearAll`, and
  `ChallengeStore.ClearPending`.
- Stale Bedrock references in `internal/ai/processor.go` comments and the README
  prerequisites, left over from the v0.5.0 move to inferd + openai-compat.

## [0.6.0] - 2026-07-17

### Added
- Native secret storage via `pass` (passwordstore.org): `WEBEX_BOT_TOKEN`,
  `WMCP_TOKEN`, `OPENAI_API_KEY` read from the `remoteclaw/` store prefix
  (strict key allowlist), overriding `.env`; falls back to `.env`/env with a
  warning when `pass` is unavailable
  ([ADR 0003](docs/adr/0003-secret-storage-and-config-lockdown.md))
- Config/secret lockdown (`security.lockdown`, default true): an in-process
  guard that denies the agent's file tools access to config/`.env`/secret store
  and best-effort-denies secret-reading commands. Best-effort defense-in-depth,
  not a hard boundary — see [ADR 0004](docs/adr/0004-privilege-separated-executor.md).
  Opt out with `security.lockdown: false`
- Documented security posture ([ADR 0004](docs/adr/0004-privilege-separated-executor.md)):
  RemoteClaw runs with the installing user's privileges by design; the security
  layers are best-effort and explicitly do not prevent an authenticated attacker
  from delivering/running a payload. Retires the earlier "airtight privilege
  separation" aspiration as unbuildable given the run-as-user model.
- `remoteclaw install --user <account>` to run the service under a dedicated
  low-privilege account
- Installer prompts (secure defaults): lock down config/secrets, store secrets
  in `pass`, enable the destructive-command challenge, restrict allowed emails
- Local inference via the `inferd` daemon (`github.com/3rg0n/inferd/clients/go`)
  over a Unix socket / Windows named pipe — new default local provider
  ([ADR 0001](docs/adr/0001-inference-via-inferd-and-openai-compat.md))
- Remote inference via any OpenAI-compatible endpoint (`openai-compat` provider,
  official `github.com/openai/openai-go`): Ollama `/v1`, mantle/Bedrock-as-OpenAI,
  vLLM, LM Studio, LocalAI, OpenAI — configured with `ai.openai_base_url`
- Passthrough mode (`ai.mode: passthrough`): execute inbound messages directly
  as commands with no local inference, for a remote AI driving the machine;
  all guardrails remain active
  ([ADR 0002](docs/adr/0002-passthrough-mode.md))
- MAESTRO threat model report (`THREAT_MODEL.md`) covering all 7 layers
- Prompt injection defense: XML delimiter wrapping for user input and tool output
- Tool output sanitization and truncation (32KB limit) before LLM feedback
- Circuit breaker: agentic loop aborts after 3 consecutive tool errors
- Global processing timeout (5 minutes) for agentic tool-call loops
- Challenge-response brute-force protection (max 3 attempts per space)
- Audit log secret scrubbing via regex (API keys, tokens, private keys)
- Audit log field truncation (10KB per field) and tool parameter logging
- Symlink bypass protection via `filepath.EvalSymlinks()` in filesystem executor
- Conversation history TTL cleanup (24-hour idle expiry)
- Root/admin detection warning at startup
- Audit logging enforcement warning when security features lack audit_log
- 15+ new dangerous command patterns: command substitution, env injection,
  kernel modules, reverse shells, privileged containers, scheduled execution
- SHA256 checksum verification in both install.sh and install.ps1
- linux/arm64 and darwin/amd64 build targets in Makefile and CI

### Changed
- Install model realigned to run as the installing user (ADR 0004):
  `remoteclaw install` now defaults to a **per-user** service — systemd `--user`
  on Linux, a LaunchAgent on macOS, and a **run-at-login Scheduled Task** on
  Windows (no service signing required). `install --system` opts into the old
  system-service/headless path. Installers no longer create a dedicated
  low-privilege system account or root-owned config by default.
- Inference architecture: replaced the embedded Ollama + AWS Bedrock SDKs and
  the `auto`/`local`/`bedrock` provider scheme with `inferd` (local) and
  `openai-compat` (remote); provider resolves to `openai-compat` when
  `ai.openai_base_url` is set, otherwise `inferd`
  ([ADR 0001](docs/adr/0001-inference-via-inferd-and-openai-compat.md))
- GitHub Actions pinned to full commit SHAs (all 6 actions)
- AI temperature capped at 0.3 in config validation for security consistency
- Conversation history size cap reduced from 512KB to 128KB
- Bedrock deserialization failures now logged at WARN instead of silently ignored
- System prompt hardened with mandatory safety constraints section

### Removed
- Embedded Ollama SDK (`github.com/ollama/ollama`) and AWS Bedrock runtime SDK
  (`github.com/aws/aws-sdk-go-v2/*`) dependencies, the `aws:` config block, and
  the `ai.ollama_host` setting (superseded by inferd + openai-compat)

### Fixed
- TOCTOU race in challenge-response: lock acquired before scrypt verification
- Potential symlink traversal bypass in sensitive path checks
