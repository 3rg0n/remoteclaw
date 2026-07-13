# RemoteClaw — AI-powered remote system control via Webex

RemoteClaw is a local agent that lets you remotely control a system via a Webex bot, powered by AI. Send natural language commands through Webex (e.g., "restart the nginx service", "check disk usage") and the AI interprets them, executes them on the local machine, and reports back the results.

## How It Works

```
 Webex User                    Webex Cloud                   Local Machine
┌──────────┐   message    ┌─────────────────┐  Mercury WS  ┌──────────────┐
│  Webex    │─────────────►│  Webex Messaging │─────────────►│   RemoteClaw  │
│  Client   │◄─────────────│  Platform        │◄─────────────│              │
└──────────┘   response   └─────────────────┘   REST API   │  ┌────────┐  │
                                                            │  │ AI     │  │
                                                            │  │ Engine │  │
                                                            │  └───┬────┘  │
                                                            │      │       │
                                                            │  ┌───▼────┐  │
                                                            │  │ Shell  │  │
                                                            │  │ Exec   │  │
                                                            │  └────────┘  │
                                                            └──────────────┘
```

1. You message the Webex bot (e.g., "check disk space")
2. RemoteClaw receives the message via Mercury WebSocket
3. The AI engine interprets the request and decides which commands to run
4. Commands execute locally with safety checks
5. Results are sent back through Webex

## Prerequisites

- **Go 1.26+** (to build from source)
- **Webex account** with access to [developer.webex.com](https://developer.webex.com)
- **AI provider** (one of):
  - [Ollama](https://ollama.com) running locally (default, free)
  - AWS account with Bedrock access (for Claude)

## Creating a Webex Bot Token

RemoteClaw uses a **Webex Bot**, not an Integration. Bots use a simple access token — no OAuth redirect URIs or scopes required.

When you go to [developer.webex.com](https://developer.webex.com) → **Create a New App**, you'll see several options:

| App Type | What it's for | Use? |
|----------|--------------|------|
| **Bot** | Chatbots that post content and respond to commands | **Yes — use this** |
| Integration | OAuth apps that act on behalf of a user (requires scopes, redirect URIs) | No |
| Service App | Org-wide privileged automation | No |
| Embedded App | Apps embedded in Webex Meetings/Spaces UI | No |
| Guest Issuer | Temporary access for non-Webex users | No |

### Step-by-step

1. Go to [developer.webex.com/my-apps](https://developer.webex.com/my-apps)
2. Click **Create a New App**
3. Select **Create a Bot**
4. Fill in the form:
   - **Bot Name** — Display name shown in Webex (e.g., "RemoteClaw")
   - **Bot Username** — Unique identifier, becomes `username@webex.bot` (e.g., `remoteclaw-prod`). Cannot be changed later.
   - **Icon** — Upload a 512x512 PNG/JPEG or pick a default
   - **App Hub Description** — e.g., "AI-powered remote system administration"
5. Click **Add Bot**
6. **Copy the Bot Access Token** — this is shown only once. If you lose it, you'll need to regenerate it from the bot's settings page.

This token goes into your `.env` file or config as `WEBEX_BOT_TOKEN`.

### Where to Create the Bot

There are two approaches depending on your situation:

#### Option A: Work Org (if your organization allows it)

If your company permits creating bots on [developer.webex.com](https://developer.webex.com), log in with your **work email** and create the bot there. The bot lives in your org, and anyone in your org can find and message it.

#### Option B: Personal Account (cross-org)

If your work org restricts bot creation, you can create the bot under a **personal Webex account**:

1. Sign up at [developer.webex.com](https://developer.webex.com) with a personal email (Gmail, etc.)
2. Create the bot under that account
3. Run RemoteClaw at home (or wherever) with that bot token
4. From your **work Webex client**, search for `yourbotname@webex.bot` and DM it

Webex bots are globally addressable — any Webex user can direct-message any bot regardless of which org created it. Set `allowed_emails` to your work email so only you can use it.

> **Note**: Some organizations disable external communications in Webex Control Hub. If your IT admin has blocked messaging outside your org, cross-org bots won't work. Test by trying to message any external Webex user first.

### Adding the Bot to a Space

- **Direct messages (1:1)**: Search for the bot by its `@webex.bot` username in Webex and send it a message directly.
- **Group spaces**: Add the bot to a room as a member. In group rooms, you must @mention the bot to trigger it (e.g., `@RemoteClaw check disk space`). The bot automatically strips the mention before processing.

> **Group room restriction**: When the bot is in a group room, only users listed in `allowed_emails` can interact with it. If no `allowed_emails` are configured, the bot will not respond to anyone in group rooms.

## Quick Start

### Automated Install

**Linux/macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.sh | bash
```

**Windows (PowerShell as Admin):**

```powershell
irm https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.ps1 | iex
```

The installer downloads the binary, installs Ollama + model, walks you through configuration, and registers RemoteClaw as a system service.

### Manual Install

### 1. Build

```bash
go build -o remoteclaw ./cmd/remoteclaw/
```

### 2. Configure

Copy the example config and create a `.env` file:

```bash
cp config/config.example.yaml config.yaml
```

Create a `.env` file in the working directory:

```bash
WEBEX_BOT_TOKEN=YourBotAccessTokenHere
# CHALLENGE=<value>                 # Optional: challenge for destructive command confirmation
# OPENAI_API_KEY=...                # Optional: bearer token for an openai-compat endpoint
```

Edit `config.yaml` to set your allowed emails:

```yaml
mode: native

webex:
  bot_token: "${WEBEX_BOT_TOKEN}"
  allowed_emails:
    - "you@company.com"
    - "teammate@company.com"
```

### 3. Start the AI backend

- **inferd (default):** install and start the [inferd](https://github.com/3rg0n/inferd)
  daemon; it loads and holds the model warm. RemoteClaw connects to its socket.
- **openai-compat:** set `ai.openai_base_url` (e.g. `http://localhost:11434/v1`
  for a running Ollama) — no local daemon needed beyond that endpoint.

### 4. Run

```bash
# Foreground
./remoteclaw run --config config.yaml

# Or install to run automatically (default: per-user, runs as YOU)
./remoteclaw install --config config.yaml
./remoteclaw status
./remoteclaw uninstall
```

**Runtime model.** RemoteClaw runs with **your** privileges — it is remote hands for
your own machine (see [ADR 0004](docs/adr/0004-privilege-separated-executor.md)).
`install` defaults to a **per-user** service:

- **Linux** — a systemd `--user` service (runs in your user session).
- **macOS** — a LaunchAgent (runs as you).
- **Windows** — a **run-at-login Scheduled Task** (starts in your session at logon;
  locking the screen keeps it running, logging off stops it). Windows has no
  unsigned-service path, so a Scheduled Task is used instead of a service.

For a headless / always-on deployment, use `install --system` (Linux/macOS system
service; optionally `--user <account>` for a dedicated service account). The
installer scripts (`install.sh` / `install.ps1`) set this up interactively.

## Connection Modes

### Native Mode (default)

Connects directly to Webex via Mercury WebSocket. Requires a bot token.

```yaml
mode: native
webex:
  bot_token: "${WEBEX_BOT_TOKEN}"
  allowed_emails:
    - "admin@company.com"
```

### WMCP Mode

Connects through a WMCP (Webex Message Control Protocol) relay server. Useful when multiple RemoteClaw agents need to be managed through a central backend.

```yaml
mode: wmcp
wmcp:
  endpoint: "wss://wmcp.example.com/ws"
  token: "${WMCP_TOKEN}"
```

The WMCP client handles authentication, heartbeats (every 30s), and automatic reconnection with exponential backoff.

## AI Providers

RemoteClaw talks to one of two inference transports, both behind the same
interface:

| Provider | Config | Where the model lives | Requirements |
|----------|--------|-----------------------|--------------|
| `inferd` (local, default) | `provider: "inferd"` (or empty) | The [inferd](https://github.com/3rg0n/inferd) daemon owns the warm model | inferd installed and running (Unix socket / named pipe) |
| `openai-compat` (remote) | `provider: "openai-compat"` + `openai_base_url` | Any OpenAI-compatible HTTP endpoint | A reachable base URL |

**Provider resolution:** an explicit `provider` wins; if left empty, RemoteClaw
uses `openai-compat` when `openai_base_url` is set, otherwise `inferd`.

`openai-compat` covers the whole OpenAI-compatible ecosystem — point
`openai_base_url` at any of:

- **Ollama:** `http://localhost:11434/v1`
- **mantle / Bedrock-as-OpenAI:** `https://mantle.internal/v1`
- **OpenAI / Anthropic OpenAI API:** the vendor base URL + `openai_api_key`
- **vLLM, LM Studio, LocalAI:** their local `/v1` endpoint

> Bedrock is no longer a built-in provider: reach it through an OpenAI-compatible
> gateway (mantle) via `openai-compat`, so RemoteClaw itself needs no AWS
> credentials. See [ADR 0001](docs/adr/0001-inference-via-inferd-and-openai-compat.md).

### Interpret vs. passthrough (`ai.mode`)

- **`interpret` (default):** the local AI interprets each message and drives the
  tools — the original behavior.
- **`passthrough`:** the inbound message is executed directly as a command with
  no local inference (Webex-as-SSH), for when a *remote* AI is the brain. All
  guardrails — dangerous-command checker, challenge-response, rate limit, strict
  allowlist, audit logging — remain fully active. See
  [ADR 0002](docs/adr/0002-passthrough-mode.md).

## Security

### Dangerous Command Blocking

Enabled by default (`dangerous_commands: true`). Blocks commands matching dangerous patterns before execution:

- Recursive root deletion (`rm -rf /`, `del /s /q C:\`)
- Disk formatting (`mkfs`, `format`, `dd of=/dev/`)
- Fork bombs
- Privilege escalation (`sudo`, `runas`, `su -`)
- Remote code execution via pipe (`curl ... | sh`)
- System shutdown/reboot

When a command is blocked, the AI is told why and relays the explanation to the user.

### Challenge-Response Confirmation

When `CHALLENGE` is set, destructive commands require the user to reply with the correct challenge response. The confirmation expires after 2 minutes.

```bash
# .env
CHALLENGE=<generated by installer>
```

Flow:
1. User: "delete all log files older than 30 days"
2. AI tries `rm -rf /var/log/old/` — command is flagged
3. Bot: "This command requires confirmation. Reply with the challenge response to proceed."
4. User: <challenge response>
5. Command executes and results are returned

### Rate Limiting

Per-space token-bucket rate limiter. Default: 10 requests/minute with burst of 3.

```yaml
security:
  rate_limit_per_min: 10  # 0 = disabled
```

### Allowed Emails

Controls who can interact with the bot. Case-insensitive matching.

```yaml
webex:
  allowed_emails:
    - "admin@company.com"
    - "ops-team@company.com"
```

- **Empty list + direct message**: Anyone can message the bot
- **Empty list + group room**: No one can use the bot (strict enforcement in rooms)
- **Populated list**: Only listed emails can interact, in any space type

### Audit Logging

When enabled, writes NDJSON audit entries to date-stamped files with automatic 30-day retention.

```yaml
security:
  audit_log: "/var/log/remoteclaw/audit"  # Creates audit-2026-02-18.jsonl, etc.
```

Each entry records: timestamp, email, space ID, raw message, tool calls made, response, duration, and any errors.

## Configuration Reference

All string values support `${ENV_VAR}` expansion. If a `.env` file exists in the working directory, its values take precedence over system environment variables.

```yaml
mode: native                        # "native" or "wmcp"

webex:
  bot_token: "${WEBEX_BOT_TOKEN}"   # Bot access token from developer.webex.com
  allowed_emails: []                # Email allowlist (empty = allow all in direct, deny all in rooms)

wmcp:
  endpoint: ""                      # WMCP WebSocket endpoint
  token: "${WMCP_TOKEN}"            # WMCP authentication token

ai:
  provider: ""                      # "" (default→inferd) | "inferd" | "openai-compat"
  mode: "interpret"                 # "interpret" (AI drives) | "passthrough" (run message as command)
  model: ""                         # openai-compat model name; ignored by inferd
  temperature: 0.2                  # 0.0–1.0 (capped at 0.3)
  max_tokens: 4096                  # Max response tokens
  max_iterations: 10                # Max tool-call loops per request
  inferd_socket: ""                 # optional socket/pipe path override (empty = platform default)
  openai_base_url: ""               # set → selects openai-compat (e.g. http://localhost:11434/v1)
  openai_api_key: "${OPENAI_API_KEY}"  # bearer token for openai-compat (may be empty locally)

security:
  dangerous_commands: true          # Enable dangerous command blocking
  audit_log: ""                     # Audit log base path (empty = disabled)
  rate_limit_per_min: 10            # Requests per minute per space (0 = disabled)
  challenge: "${CHALLENGE}"         # Challenge token for destructive command confirmation (empty = disabled)

execution:
  default_timeout: "30s"            # Default command timeout
  max_timeout: "5m"                 # Maximum allowed timeout
  shell: ""                         # Auto-detect: sh on Linux/macOS, powershell on Windows

logging:
  level: "info"                     # "debug", "info", "warn", "error"
  format: "json"                    # "json" or "console"
  file: ""                          # Log file path (empty = stdout only)

health:
  enabled: true                     # Enable health check endpoint
  addr: "127.0.0.1:9090"           # Health check listen address
```

## CLI Commands

```
remoteclaw run        Start the agent in the foreground
remoteclaw install    Install as a system service and start it
remoteclaw uninstall  Stop and remove the system service
remoteclaw status     Show service status
remoteclaw version    Print version information

Flags:
  --config string   Path to config file (default "config.yaml")
```

## Uninstall

**Linux:**

```bash
remoteclaw uninstall
sudo rm /usr/local/bin/remoteclaw
sudo rm -rf /etc/remoteclaw/
```

**macOS:**

```bash
remoteclaw uninstall
sudo rm /usr/local/bin/remoteclaw
sudo rm -rf /usr/local/etc/remoteclaw/
```

**Windows (PowerShell as Admin):**

```powershell
remoteclaw uninstall
Remove-Item "C:\ProgramData\remoteclaw" -Recurse -Force
```

## Development

```bash
# Build
go build ./cmd/remoteclaw/

# Run all tests with race detector
go test -race -count=1 ./...

# Lint
golangci-lint run ./...

# Run a single test
go test -run TestIntegration_ChallengeResponse ./internal/agent/
```

## License

See [LICENSE](LICENSE) for details.
