# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WCCA (Webex Command & Control Agent)** — A remote system control tool that allows users to manage a local machine via a Webex bot, powered by AI.

### How It Works

1. The user creates a Webex bot and runs WCCA locally with the bot token
2. WCCA connects to Webex via Mercury WebSocket (native mode) or a WMCP relay server
3. The user talks to the Webex bot (e.g., "restart service")
4. The AI engine interprets the command, executes it on the local machine, and reports back
5. Essentially "remote hands" for system administration via chat

## Project Status

Implementation complete. All core subsystems are functional.

## Architecture

- **WCCA Agent** (`internal/agent/`) — Main orchestrator: message handling, conversation history, rate limiting, challenge-response, audit logging
- **AI Engine** (`internal/ai/`) — Ollama (local) and AWS Bedrock providers, agentic tool-call loop, system prompt
- **Executor** (`internal/executor/`) — 7 tools: execute_command, read_file, write_file, list_dir, list_processes, kill_process, system_info
- **Connect** (`internal/connect/`) — Native Webex mode (Mercury WS + REST API) and WMCP WebSocket client mode
- **Security** (`internal/security/`) — Dangerous command checker, per-space rate limiter, challenge-response confirmation
- **Logging** (`internal/logging/`) — Structured logging (zerolog) and NDJSON audit logging with 30-day retention
- **Config** (`internal/config/`) — YAML + env var + .env file configuration
- **Service** (`internal/service/`) — System service install/uninstall via kardianos/service

## Build & Test

```bash
go build ./cmd/wcca/              # Build
go test -race -count=1 ./...      # Test with race detector
golangci-lint run ./...            # Lint
```

## Key Patterns

- All env-sensitive config values support `${ENV_VAR}` expansion
- `.env` file (if present) takes precedence over system env vars via godotenv
- Native mode uses `webex-message-handler` for Mercury WebSocket receiving
- In group rooms, `allowed_emails` is strictly enforced (empty = deny all)
- Challenge-response: when `CHALLENGE` env var is set, dangerous commands require user confirmation
