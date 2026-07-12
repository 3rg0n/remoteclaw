# 0002. Passthrough mode: execute inbound messages directly, no local inference

- Status: accepted
- Date: 2026-07-12

## Context

RemoteClaw has two use cases:

1. **Interpret (existing):** a human types natural language; the local AI
   interprets intent and drives the tools.
2. **Remote AI driving (new):** an external AI agent is the "brain" and
   RemoteClaw is its hands on the machine. In this case a *second* local model
   interpreting the remote AI's already-explicit commands adds latency, cost,
   and a lossy translation step. The natural model is to treat the Webex space
   like an SSH session — the inbound message *is* the command.

We need to support use case 2 without a local inference round-trip, while not
weakening the security posture that makes a remote-execution agent acceptable to
run at all.

## Decision

Add `ai.mode: passthrough` (default `interpret`). In passthrough mode:

- No inference client is constructed; `ai.provider` / model settings are ignored.
- An inbound message is executed directly as an `execute_command` call.
- **Every existing guardrail remains active and unchanged.** The command routes
  through the *same* `executeToolGuarded` path the AI loop uses, so:
  - the dangerous-command checker blocks the same patterns before execution;
  - challenge-response confirmation applies identically — a blocked command
    registers a pending challenge keyed by the space and returns a confirmation
    prompt rather than executing;
  - the per-space rate limiter and the (strict, in group rooms) email allowlist
    gate the message before it is ever treated as a command;
  - every execution is written to the audit log, tagged `[passthrough]`.

The guarded execution path was factored into a single shared function so that
interpret and passthrough modes cannot drift apart in what they enforce.

## Consequences

- **Easier:** a remote AI (or a human who wants raw shell-over-chat) can drive
  the machine with no local-model latency or cost, and no second model
  potentially mis-transcribing an explicit command.
- **Harder / risks:** passthrough removes the AI's interpretation layer, which
  in interpret mode also acts as a soft safety filter (the system prompt refuses
  privilege escalation, exfiltration, etc.). In passthrough the *only* safety
  net is the deterministic guardrail stack above. This is a deliberate trade:
  the deterministic checks are the guarantees we actually rely on; the model's
  judgment was never a guarantee. Operators must understand that in passthrough,
  authorization rests entirely on the allowlist + challenge-response + dangerous
  checker — so those must be configured. A startup warning is logged whenever
  passthrough is enabled.
- Because both modes share one guarded execution path, a future change to the
  guardrails automatically covers both; there is no passthrough-specific bypass
  to audit separately.

## Threat notes (MAESTRO-aligned)

- **Spoofing / authorization:** unchanged from interpret mode — the allowlist is
  enforced before the message is handled, strictly in group rooms (empty list =
  deny all). Passthrough does not relax who may talk to the bot.
- **Elevation of privilege:** the dangerous-command checker (privilege
  escalation, disk-wipe, fork bomb, remote-pipe-to-shell, shutdown) runs
  identically; challenge-response still gates confirmed-dangerous execution.
- **Repudiation:** every passthrough execution is audit-logged with the same
  fields as an AI-driven tool call.
- **New residual risk:** loss of the model's soft refusal layer. Mitigation:
  the deterministic guardrails are the actual control; operators enabling
  passthrough should treat allowlist + challenge-response as mandatory, not
  optional.
