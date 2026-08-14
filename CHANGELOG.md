# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.7.0] - 2026-08-14

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
- **`rm -rf /*` was not blocked, and it is the form that actually works.** The
  root-deletion and `chmod 777` rules matched a bare `/` — which is the one
  spelling GNU coreutils refuses on its own, via the `--preserve-root` default
  (`rm -rf /` exits 1 without touching anything). The spellings that do destroy a
  system were allowed: `rm -rf /*` (the canonical one — the shell expands the glob
  into every entry in `/`, so `rm` never sees a `/` operand and no failsafe
  applies), `rm -rf --no-preserve-root /`, `chmod 777 /*`, and the quoted forms.
  `chmod` has no failsafe at all; `--no-preserve-root` is its documented default.
  Verified on coreutils 8.32. So the rule fired on the harmless variant and missed
  the dangerous ones. **This was live in every released version** — `git log -S`
  finds no glob form anywhere in the history of `internal/security/`. Rules now
  match the *operand* (`rootTarget`: bare, globbed, dot, and quoted forms) and
  skip over flags and earlier operands to find it (`operandRun`), which also closes
  `rm -r --verbose -f /` and `rm --recursive --force /` — the four flag-ordered
  `rm` rules enumerated `-r`-then-`-f` and `-f`-then-`-r` and nothing in between —
  and `rm -rf backup /*`, where the root target is not the first operand and these
  commands act on every operand they are given. The glob spellings are a character
  run rather than an enumeration, so `rm -rf /**` — which expands to every entry in
  `/` exactly as `/*` does — is blocked along with `/***`, `/*/`, `/*/*`, `/.*`,
  `//*`, and the quoted variants; enumerating them one at a time was the original
  defect a third time, and an independent review caught it after the first two fixes
  had landed. The four rules collapse into one covering strictly more, verified
  differentially against both former patterns over 57,024 generated invocations
  (zero inputs lost). `chown` and `chgrp` on root are now covered at all
  ([ADR 0009](docs/adr/0009-deny-rules-match-the-operand-not-the-flags.md)).
- **Two more command positions reached the `exec` builtin unblocked**: a leading
  redirection (`>out exec sh`, `2>/dev/null exec sh`, and the space-separated
  `> out exec sh` — verified to reach the builtin and replace the shell) and a
  backtick substitution (`` echo `exec sh` ``). Redirections and `coproc` join the
  skipped-prefix set and the backtick joins the separator set. The redirection
  form consumes its *target*, not just the operator, which matters in both
  directions: a version stopping at the operator missed the space-separated
  spelling and left the target in command position, refusing `ls; > exec.log` — a
  truncate idiom that executes nothing. A `case` branch
  (`case x in y) exec sh;; esac`) remains knowingly uncovered: matching it requires
  treating `)` as a separator, which matches the end of every `$(…)` and would
  refuse `docker $(flags) exec web sh` — the trade
  [ADR 0007](docs/adr/0007-deny-rules-anchored-to-command-position.md) rejected
  ([ADR 0009](docs/adr/0009-deny-rules-match-the-operand-not-the-flags.md)).
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
  `until`/`do` and `!`. The brace-group opener counts only when followed by
  whitespace, and a closing `}` is not a command position at all — folding both
  into the separator class instead makes `}` match the end of a `${VAR}` expansion
  and refuses `docker ${FLAGS} exec web sh`, trading the under-match for exactly
  the false positives the anchor exists to avoid. All 22 false-positive cases from
  [ADR 0007](docs/adr/0007-deny-rules-anchored-to-command-position.md) still pass —
  `docker exec` and `echo $(date)` are still allowed — and three new tables pin the
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
- **Bumped every stale GitHub Actions pin to its current release, still pinned by
  commit SHA.** `actions/checkout` v4.2.2→v7.0.1, `actions/setup-go` v5.5.0→v7.0.0,
  `actions/upload-artifact` v4.6.2→v7.0.1, `actions/download-artifact` v4.3.0→v8.0.1,
  `softprops/action-gh-release` v2.2.2→v3.0.2. The v4/v5 lines run on the Node 20
  Actions runtime, which is deprecated; every new pin declares `node24`. Each SHA
  was resolved from the release tag through the GitHub API and verified to be a
  commit object — two of the tags (`action-gh-release`, `golangci-lint-action`) are
  *annotated tags*, whose ref SHA is the tag object rather than the commit, so
  pinning the ref SHA directly would have pinned something a `git checkout` cannot
  reach. `golangci/golangci-lint-action` was already current at v9.3.0.
  Breaking changes in the majors were checked against what this repo actually
  uses: `setup-go` v6 forces `GOTOOLCHAIN=local` (no effect — `go.mod` declares
  `go 1.26.0` and no `toolchain` directive), `download-artifact` v8 promotes a
  digest mismatch from a warning to a job failure (kept — these are the release
  binaries the checksums are computed over), `upload-artifact` v7 adds an `archive`
  input left at its `true` default, and `checkout` v7 blocks fork-PR checkout on
  `pull_request_target`/`workflow_run`, neither of which this repo triggers on.
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
- **The `openai-compat` live integration tests no longer fail when an unrelated
  server holds port 8080.** Their skip guard did a bare TCP dial to
  `localhost:8080`, which only proves *something* is listening — and 8080 is a
  popular port. Any other local server (in the case that surfaced this, a wasm
  runtime) made the guard pass, and the tests then ran against the wrong server
  and failed with a confusing `405 Method Not Allowed`. That is a false failure
  reporting a broken client when the real condition is "the mock is not running",
  and it is the failure mode a skip guard exists to prevent. The guard now POSTs
  to the real `/chat/completions` path and treats only 404/405 as "not an
  OpenAI-dialect endpoint", so a foreign listener skips with a message naming the
  cause. Verified in all four directions: skips when nothing listens, skips on a
  foreign listener, runs and passes against a real endpoint, and **still fails
  when the client is broken** — the last one matters, since a guard that
  over-skips would silently stop testing anything.
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
