# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
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
- GitHub Actions pinned to full commit SHAs (all 6 actions)
- Ollama SDK upgraded from v0.18.2 to v0.20.2
- AI temperature capped at 0.3 in config validation for security consistency
- Conversation history size cap reduced from 512KB to 128KB
- Bedrock deserialization failures now logged at WARN instead of silently ignored
- System prompt hardened with mandatory safety constraints section

### Fixed
- TOCTOU race in challenge-response: lock acquired before scrypt verification
- Potential symlink traversal bypass in sensitive path checks
