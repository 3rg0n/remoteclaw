# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
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
