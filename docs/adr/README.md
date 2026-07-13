# Architecture Decision Records

This directory records the *why* behind cross-cutting or hard-to-reverse
architectural decisions in RemoteClaw. ADRs are immutable once accepted; to
change a decision, add a new ADR that supersedes the old one and update the old
one's status.

Format: [Nygard short form](https://github.blog/engineering/architecture-optimization/why-write-adrs/)
(Context / Decision / Consequences).

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-inference-via-inferd-and-openai-compat.md) | Inference via inferd (local) and OpenAI-compatible (remote); drop embedded Ollama and Bedrock SDKs | accepted |
| [0002](0002-passthrough-mode.md) | Passthrough mode: execute inbound messages directly, no local inference | accepted |
| [0003](0003-secret-storage-and-config-lockdown.md) | Secret storage via `pass`, and OS-enforced config/secret lockdown | accepted |
| [0004](0004-privilege-separated-executor.md) | Security posture: best-effort defense-in-depth, run as the installing user | accepted |
