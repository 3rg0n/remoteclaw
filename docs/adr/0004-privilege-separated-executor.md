# 0004. Privilege-separated executor for airtight config/secret isolation

- Status: proposed
- Date: 2026-07-13

## Context

ADR 0003 introduced the config/secret lockdown with two layers: OS file
ownership (run the service as a dedicated low-privilege user) and an in-process
guard (`internal/executor.Guard`). During implementation a fundamental limit
surfaced:

**RemoteClaw is a single process running as a single OS user.** The service must
read its own `config.yaml` and secrets at startup, but `execute_command` and the
file tools run *in that same process, as that same uid*. Therefore file
ownership cannot simultaneously (a) let the service read its config and (b) deny
the agent's own `execute_command "cat config.yaml"` — both are the same user.

The in-process guard closes the easy paths (file tools hard-deny protected
paths; `execute_command` pattern-denies obvious secret reads), but a process
that can run arbitrary shell can evade any in-process command-pattern match
(base64, copy-then-read, `printf` indirection, interpreters). So the goal "even
as admin, no command can read the secrets" is **not** achievable with the
current single-uid executor. This ADR proposes the change that makes it true.

## Decision (proposed)

Introduce **privilege separation** in the executor:

- The **service core** continues to run as a low-privilege service user
  (`remoteclaw-svc`) that *can* read config/secrets at startup.
- **All agent-driven side effects** — `execute_command` child processes and the
  `read_file`/`write_file`/`list_dir` operations — are performed as a **distinct
  second identity** (`remoteclaw-exec`) that has **no read access** to config,
  `.env`, or the secret store.
  - Unix: spawn child processes with `syscall.SysProcAttr{Credential: ...}` for
    the exec uid; perform file-tool I/O via a small helper that drops to that
    uid (or a separate short-lived helper process), never in the privileged
    core.
  - Windows: launch commands with `CreateProcessAsUser` under a restricted
    token / dedicated low-privilege account; deny that account on the config
    ACL while granting the service-core account read.

With this, `execute_command "cat config.yaml"` (and any variant) is denied by
the **OS** regardless of the command string — the guarantee no longer depends on
enumerating dangerous patterns.

The installer provisions both accounts and sets ownership/ACLs so config is
readable by `remoteclaw-svc` and denied to `remoteclaw-exec`.

## Consequences

- **Airtight guarantee.** "Even as the admin who installed it, RemoteClaw cannot
  be instructed to read its secrets/settings via any command" becomes true,
  because the OS — not a regex — enforces it. Editing settings requires local
  root/Administrator.
- **Cost / complexity.** Cross-platform privilege dropping is non-trivial,
  especially on Windows (token creation, `SeAssignPrimaryTokenPrivilege`, the
  logon-account/`LogonUser` dance). File tools must route I/O through the exec
  identity, which changes `internal/executor/filesystem.go`.
- **Interaction with pass.** The exec identity must not reach the secret store
  either; the store belongs to the service-core account. This also resolves the
  ADR 0003 note about the store vs. service-account tension.
- **Supersedes** the aspiration in ADR 0003 that OS file ownership alone yields
  the airtight guarantee. ADR 0003's shipped behavior (low-priv service user +
  in-process guard as defense-in-depth) stands as the interim state until this
  is implemented.

## Status note

Proposed, not yet implemented. The current release ships ADR 0003's layers and
documents the in-process command guard honestly as defense-in-depth. This ADR is
the tracked follow-up for the airtight version.
