# 0005. Authorization is a single choke point in the agent, not a Mode concern

- Status: accepted
- Date: 2026-08-08

## Context

Through v0.6.0, the sender allowlist was implemented inside one `connect.Mode`.
`NewNativeMode` took `allowedEmails`, built a `connect.Allowlist`, and checked
`IsAllowedInRoom` inside its Mercury message callback. `NewWMCPMode` took no
allowlist and performed no check.

The consequence was a security gap, not a style problem: in `wmcp` mode
`webex.allowed_emails` was silently ignored, and any sender the relay forwarded
reached the rate limiter, the challenge store, and ultimately the executor. The
config field was present, documented, and accepted — it just did nothing. A
second, quieter defect compounded it: `WMCPMode` left `IncomingMessage.RoomType`
empty, and `Allowlist.IsAllowedInRoom` treats anything other than `"group"` as a
1:1 direct message, where an empty allowlist means *allow anyone*. So even had
the check been wired into `WMCPMode` unchanged, the default configuration would
have authorized every relayed sender.

`THREAT_MODEL.md` asserted that "the email allowlist is applied in the connect
layer (`native.go` `IsAllowedInRoom`) before `messageHandler` runs." That was
accurate for the code it named and misleading about the system: it described one
implementation as though it were an invariant.

The structural problem is that authorization lived in an interface with two
implementations and no mechanism to require it. `Mode` is the extension point —
adding a transport means writing a new `Mode` — and nothing in the interface, the
compiler, or the test suite forced a new one to authorize anything. `WMCPMode`
was not an oversight that better review would have caught; it is what the
architecture made easy.

Two options were considered:

- **(a) Give `WMCPMode` an allowlist too.** Smallest diff, closes today's gap,
  and leaves the next `Mode` exactly as free to forget. It also duplicates the
  authz decision, so the two copies can drift.
- **(b) Lift authorization out of the Modes into the agent.** Larger diff (it
  changes `NewNativeMode`'s signature), but there is then one decision point,
  and it sits on the only path every Mode feeds.

## Decision

**Authorization happens exactly once, in `Agent.authorize`, called at the top of
`Agent.messageHandler`.** Option (b).

- `Agent` owns the `connect.Allowlist`, built from `cfg.Webex.AllowedEmails` in
  `agent.New` regardless of `cfg.Mode`.
- `authorize` runs **before** the rate limiter, the challenge-response check, and
  any executor dispatch. An unauthorized sender consumes no rate-limit tokens,
  cannot register or answer a challenge, and receives no reply — nothing that
  would confirm a bot is listening.
- **It fails closed.** A nil allowlist denies. Authorization must not depend on a
  struct field having been remembered at construction.
- **`Mode` implementations carry no authorization logic.** `NewNativeMode` no
  longer accepts `allowedEmails`; the allowlist field and the callback check are
  gone. A Mode's contract is to report provenance honestly — `Email` and
  `RoomType` — and nothing more.
- **`WMCPMode` reports `RoomType: "group"`.** The WMCP envelope has no room-type
  field, and a relay cannot prove a message came from a 1:1 space. `"group"` is
  the strict interpretation, so an unset `allowed_emails` denies every relayed
  sender instead of admitting all of them. Choosing the permissive default here
  would mean trusting the relay to make an authorization-relevant claim it has no
  way to substantiate.
- `agent.New` logs at startup whether the allowlist is populated, so an empty
  list is a visible operational fact rather than a silent default.

`webex.allowed_emails` keeps its name despite now governing all modes. Renaming
it (to `security.allowed_senders`, say) would break every existing config for a
cosmetic gain; the field is documented as applying to every mode instead.

## Consequences

**Breaking change for `wmcp` operators.** A `wmcp` deployment with an empty
`allowed_emails` accepted every relayed sender before this change and accepts
none after it. This is the fix working as intended — the previous behavior was
unauthenticated remote code execution gated only by possession of a relay token —
but it will stop a working deployment until emails are listed. The README calls
this out at the `wmcp` config example, and the release notes flag it. Operators
who ran `wmcp` mode on ≤ v0.6.0 should review their audit log.

**A new `Mode` gets authorization for free**, and cannot opt out: it never sees
the allowlist. The class of bug this ADR closes cannot recur through the
extension point that produced it. What a Mode *can* still get wrong is
provenance — misreporting `RoomType`, or trusting a transport-supplied `Email`.
That is a narrower, more inspectable obligation than "remember to authorize," and
it is now the documented contract in both Mode implementations.

**`NewNativeMode`'s signature changed** (`allowedEmails` dropped). It is internal
(`internal/connect`), so the blast radius is this repo.

**Denials are audited.** An unauthorized message is logged as a WARN with email,
space, and room type, and written to the audit log with
`Error: "unauthorized sender"`. Rejection was previously silent in native mode
and nonexistent in wmcp mode; probing is now visible.

**Regression coverage is at the choke point, not per-Mode.** `authorize` is
table-tested across the mode/room-type matrix (wmcp-as-group, native group,
native direct, empty room type) plus a fail-closed case for a nil allowlist, and
`messageHandler` is tested end-to-end in passthrough mode so a regression would
visibly execute a command. Separately, the WMCP integration test asserts
`RoomType == "group"` on a relayed message — this was verified to fail against
the pre-fix `wmcp.go` (it reported `""`), so it is a real guard on the strictness
decision and not a tautology.

**Trust boundary 1 is now accurately documented.** `THREAT_MODEL.md` carries an
explicit correction of the superseded claim rather than a quiet edit, since the
old text was cited as evidence the control existed.
