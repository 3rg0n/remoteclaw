#!/usr/bin/env bash
set -euo pipefail

# RemoteClaw Installer — Linux / macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.sh | bash

REPO="3rg0n/remoteclaw"
RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
OLLAMA_MODEL="phi4-mini"

# --- Colors -----------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { printf "${CYAN}[info]${NC}  %s\n" "$*"; }
ok()    { printf "${GREEN}[ok]${NC}    %s\n" "$*"; }
warn()  { printf "${YELLOW}[warn]${NC}  %s\n" "$*"; }
err()   { printf "${RED}[error]${NC} %s\n" "$*" >&2; }

# --- Cleanup on exit ---------------------------------------------------
TMPDIR_INSTALL=""
cleanup() {
    if [ -n "$TMPDIR_INSTALL" ] && [ -d "$TMPDIR_INSTALL" ]; then
        rm -rf "$TMPDIR_INSTALL"
    fi
}
trap cleanup EXIT

# --- Detect OS and architecture ----------------------------------------
detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      err "Unsupported OS: $os"; exit 1 ;;
    esac

    case "$arch" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        *)              err "Unsupported architecture: $arch"; exit 1 ;;
    esac

    # Set platform-specific paths
    if [ "$OS" = "linux" ]; then
        CONF_DIR="/etc/remoteclaw"
        LOG_DIR="/var/log/remoteclaw"
    else
        CONF_DIR="/usr/local/etc/remoteclaw"
        LOG_DIR="/usr/local/var/log/remoteclaw"
    fi
    BIN_DIR="/usr/local/bin"
    BIN_PATH="${BIN_DIR}/remoteclaw"
    CONFIG_PATH="${CONF_DIR}/config.yaml"
    ENV_PATH="${CONF_DIR}/.env"
}

# --- Check sudo --------------------------------------------------------
check_sudo() {
    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    elif command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        info "Sudo access is required for installation."
        sudo -v || { err "Failed to obtain sudo. Run as root or ensure sudo is configured."; exit 1; }
    else
        err "This script requires root privileges. Please run as root."
        exit 1
    fi
}

# --- Check for existing install ----------------------------------------
check_existing() {
    if [ -f "$BIN_PATH" ]; then
        local current_version
        current_version="$("$BIN_PATH" version 2>/dev/null || echo "unknown")"
        warn "RemoteClaw is already installed at ${BIN_PATH} (${current_version})"
        printf "  Upgrade to latest? [Y/n] "
        read -r answer
        case "$answer" in
            [nN]*) info "Aborted."; exit 0 ;;
        esac
        info "Upgrading…"
    fi
}

# --- Download binary ----------------------------------------------------
download_binary() {
    local asset="remoteclaw-${OS}-${ARCH}"
    local url="${RELEASE_URL}/${asset}"
    local checksums_url="${RELEASE_URL}/CHECKSUMS.txt"

    TMPDIR_INSTALL="$(mktemp -d)"
    local tmp_bin="${TMPDIR_INSTALL}/remoteclaw"
    local tmp_checksums="${TMPDIR_INSTALL}/CHECKSUMS.txt"

    info "Downloading ${asset} from GitHub Releases…"
    if command -v curl >/dev/null 2>&1; then
        curl -fSL --progress-bar -o "$tmp_bin" "$url"
        curl -fsSL -o "$tmp_checksums" "$checksums_url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q --show-progress -O "$tmp_bin" "$url"
        wget -q -O "$tmp_checksums" "$checksums_url"
    else
        err "Neither curl nor wget found. Cannot download."; exit 1
    fi

    # Verify checksum
    if command -v sha256sum >/dev/null 2>&1; then
        local expected
        expected="$(grep "${asset}$" "$tmp_checksums" | awk '{print $1}')"
        if [ -z "$expected" ]; then
            warn "Could not find checksum for ${asset} in CHECKSUMS.txt — skipping verification"
        else
            local actual
            actual="$(sha256sum "$tmp_bin" | awk '{print $1}')"
            if [ "$expected" != "$actual" ]; then
                err "Checksum verification FAILED for ${asset}"
                err "  Expected: ${expected}"
                err "  Actual:   ${actual}"
                err "The downloaded binary may be corrupted or tampered with."
                exit 1
            fi
            ok "Checksum verified for ${asset}"
        fi
    elif command -v shasum >/dev/null 2>&1; then
        local expected
        expected="$(grep "${asset}$" "$tmp_checksums" | awk '{print $1}')"
        if [ -n "$expected" ]; then
            local actual
            actual="$(shasum -a 256 "$tmp_bin" | awk '{print $1}')"
            if [ "$expected" != "$actual" ]; then
                err "Checksum verification FAILED for ${asset}"; exit 1
            fi
            ok "Checksum verified for ${asset}"
        fi
    else
        warn "sha256sum/shasum not found — skipping checksum verification"
    fi

    $SUDO install -m 755 "$tmp_bin" "$BIN_PATH"
    ok "Installed remoteclaw → ${BIN_PATH}"
}

# --- Install Ollama -----------------------------------------------------
install_ollama() {
    if command -v ollama >/dev/null 2>&1; then
        ok "Ollama is already installed."
        return 0
    fi

    info "Ollama not found. Installing…"
    if ! curl -fsSL https://ollama.com/install.sh | sh; then
        warn "Ollama installation failed. You can install it manually later"
        warn "or configure AWS Bedrock as the AI provider instead."
        return 1
    fi
    ok "Ollama installed."
    return 0
}

# --- Start Ollama -------------------------------------------------------
start_ollama() {
    # Check if Ollama is already responding
    if curl -sf http://localhost:11434/api/version >/dev/null 2>&1; then
        ok "Ollama is already running."
        return 0
    fi

    info "Starting Ollama…"
    if [ "$OS" = "linux" ]; then
        if command -v systemctl >/dev/null 2>&1; then
            $SUDO systemctl start ollama 2>/dev/null || true
        fi
    else
        # macOS: try brew services, then fall back to manual launch
        if command -v brew >/dev/null 2>&1; then
            brew services start ollama 2>/dev/null || true
        else
            nohup ollama serve >/dev/null 2>&1 &
        fi
    fi

    # Wait for Ollama to be ready (up to 15 seconds)
    local tries=0
    while [ $tries -lt 15 ]; do
        if curl -sf http://localhost:11434/api/version >/dev/null 2>&1; then
            ok "Ollama is running."
            return 0
        fi
        sleep 1
        tries=$((tries + 1))
    done

    warn "Ollama did not start in time. You may need to start it manually."
    return 1
}

# --- Pull model ---------------------------------------------------------
pull_model() {
    if ! command -v ollama >/dev/null 2>&1; then
        return 1
    fi

    info "Pulling model ${OLLAMA_MODEL}… (this may take a few minutes on first run)"
    if ollama pull "$OLLAMA_MODEL"; then
        ok "Model ${OLLAMA_MODEL} is ready."
    else
        warn "Failed to pull model. You can run 'ollama pull ${OLLAMA_MODEL}' later."
    fi
}

# --- Challenge setup -----------------------------------------------------
encrypt_challenge() {
    # Prepare the challenge value for storage
    local passphrase="$1"
    local sentinel="REMOTECLAW_CHALLENGE_OK"

    # Generate salt and derive key
    local salt
    salt=$(openssl rand -hex 16)

    # Derive key
    local key
    key=$(echo -n "$passphrase" | openssl dgst -sha256 -mac HMAC -macopt "hexkey:${salt}" -binary | xxd -p -c 64)

    # Generate nonce
    local nonce
    nonce=$(openssl rand -hex 12)

    # Encrypt
    local encrypted
    encrypted=$(echo -n "$sentinel" | openssl enc -aes-256-gcm \
        -K "$key" -iv "$nonce" -nosalt -a 2>/dev/null || echo "")

    if [ -z "$encrypted" ]; then
        # Fallback: store directly — RemoteClaw will handle on first run.
        warn "openssl not available for challenge setup. RemoteClaw will configure on first run."
        echo "$passphrase"
        return
    fi

    # Combine and encode
    echo "${salt}${nonce}${encrypted}" | base64
}

# --- Interactive prompts -------------------------------------------------
prompt_config() {
    echo ""
    printf "${BOLD}=== RemoteClaw Configuration ===${NC}\n"
    echo ""

    # Bot token (required)
    while true; do
        printf "  Webex Bot Token (required): "
        read -r BOT_TOKEN
        if [ -n "$BOT_TOKEN" ]; then
            break
        fi
        err "Bot token is required. Get one at https://developer.webex.com/my-apps"
    done

    # Challenge secret (optional)
    printf "  Challenge secret for destructive-command confirmation (optional): "
    read -r CHALLENGE_PASSPHRASE
    CHALLENGE_PASSPHRASE="${CHALLENGE_PASSPHRASE:-}"

    # If challenge provided, set it up using the binary
    CHALLENGE_ENCRYPTED=""
    if [ -n "$CHALLENGE_PASSPHRASE" ]; then
        info "Setting up challenge…"
        # Use the installed binary to set up the challenge
        if CHALLENGE_ENCRYPTED=$("$BIN_PATH" encrypt-challenge "$CHALLENGE_PASSPHRASE" 2>/dev/null); then
            ok "Challenge configured."
        else
            # Binary doesn't have encrypt-challenge yet — store raw for now
            warn "Binary setup not available. Storing challenge — configure before production use."
            CHALLENGE_ENCRYPTED="$CHALLENGE_PASSPHRASE"
        fi
    fi

    # Allowed emails (optional)
    printf "  Allowed emails, comma-separated (optional): "
    read -r ALLOWED_EMAILS_RAW
    ALLOWED_EMAILS_RAW="${ALLOWED_EMAILS_RAW:-}"
}

# --- Create directories --------------------------------------------------
create_dirs() {
    info "Creating directories…"
    $SUDO mkdir -p "$CONF_DIR"
    $SUDO mkdir -p "$LOG_DIR"
    ok "Created ${CONF_DIR} and ${LOG_DIR}"
}

# --- Generate .env --------------------------------------------------------
generate_env() {
    info "Generating ${ENV_PATH}…"

    local env_content="WEBEX_BOT_TOKEN=${BOT_TOKEN}"
    if [ -n "$CHALLENGE_ENCRYPTED" ]; then
        env_content="${env_content}
CHALLENGE=${CHALLENGE_ENCRYPTED}"
    fi

    echo "$env_content" | $SUDO tee "$ENV_PATH" >/dev/null
    $SUDO chmod 600 "$ENV_PATH"
    ok "Created ${ENV_PATH} (mode 600)"

    # Also export to shell profile for persistence
    if [ -n "$CHALLENGE_ENCRYPTED" ] && [ "$OS" = "darwin" ]; then
        local zshenv="$HOME/.zshenv"
        if ! grep -q "CHALLENGE=" "$zshenv" 2>/dev/null; then
            echo "export CHALLENGE=\"${CHALLENGE_ENCRYPTED}\"" >> "$zshenv"
            info "Added CHALLENGE to ${zshenv}"
        fi
    elif [ -n "$CHALLENGE_ENCRYPTED" ] && [ "$OS" = "linux" ]; then
        local profile="$HOME/.profile"
        if ! grep -q "CHALLENGE=" "$profile" 2>/dev/null; then
            echo "export CHALLENGE=\"${CHALLENGE_ENCRYPTED}\"" >> "$profile"
            info "Added CHALLENGE to ${profile}"
        fi
    fi
}

# --- Generate config.yaml ------------------------------------------------
generate_config() {
    info "Generating ${CONFIG_PATH}…"

    # Build allowed_emails YAML list
    local emails_yaml=""
    if [ -n "$ALLOWED_EMAILS_RAW" ]; then
        IFS=',' read -ra emails <<< "$ALLOWED_EMAILS_RAW"
        for email in "${emails[@]}"; do
            email="$(echo "$email" | xargs)"  # trim whitespace
            if [ -n "$email" ]; then
                emails_yaml="${emails_yaml}
    - \"${email}\""
            fi
        done
    fi

    if [ -z "$emails_yaml" ]; then
        emails_yaml="
    # - \"admin@company.com\""
    fi

    local challenge_line=""
    if [ -n "$CHALLENGE_ENCRYPTED" ]; then
        challenge_line='  challenge: "${CHALLENGE}"'
    else
        challenge_line='  challenge: ""'
    fi

    $SUDO tee "$CONFIG_PATH" >/dev/null <<YAML
mode: native

webex:
  bot_token: "\${WEBEX_BOT_TOKEN}"
  allowed_emails:${emails_yaml}

ai:
  provider: "auto"
  model: "${OLLAMA_MODEL}"
  temperature: 0.2
  max_tokens: 4096
  max_iterations: 10

security:
  dangerous_commands: true
  audit_log: "${LOG_DIR}/audit"
  rate_limit_per_min: 10
${challenge_line}

execution:
  default_timeout: "30s"
  max_timeout: "5m"
  shell: ""

logging:
  level: "info"
  format: "json"
  file: ""

health:
  enabled: true
  addr: "127.0.0.1:9090"
YAML

    ok "Created ${CONFIG_PATH}"
}

# --- Install service ------------------------------------------------------
install_service() {
    info "Installing RemoteClaw as a system service…"
    if $SUDO "$BIN_PATH" install --config "$CONFIG_PATH"; then
        ok "Service installed."
    else
        warn "Service installation failed. You can run 'remoteclaw install --config ${CONFIG_PATH}' manually."
        return 1
    fi
}

# --- Verify ---------------------------------------------------------------
verify() {
    info "Checking service status…"
    if "$BIN_PATH" status 2>/dev/null; then
        ok "RemoteClaw service is running."
    else
        warn "Service may not be running yet. Check with: remoteclaw status"
    fi
}

# --- Print summary --------------------------------------------------------
print_summary() {
    echo ""
    printf "${BOLD}=== Installation Complete ===${NC}\n"
    echo ""
    echo "  Binary:     ${BIN_PATH}"
    echo "  Config:     ${CONFIG_PATH}"
    echo "  Env file:   ${ENV_PATH}"
    echo "  Audit logs: ${LOG_DIR}/"
    echo ""
    echo "  Talk to your bot in Webex — send it a message like:"
    echo "    \"What's the disk usage?\""
    echo ""
    echo "  Useful commands:"
    echo "    remoteclaw status                     Show service status"
    echo "    remoteclaw uninstall                  Remove the service"
    echo "    sudo rm /usr/local/bin/remoteclaw     Remove the binary"
    if [ "$OS" = "linux" ]; then
        echo "    sudo rm -rf /etc/remoteclaw/          Remove config"
    else
        echo "    sudo rm -rf /usr/local/etc/remoteclaw/ Remove config"
    fi
    echo ""
}

# --- Main ------------------------------------------------------------------
main() {
    echo ""
    printf "${BOLD}RemoteClaw Installer — AI-powered remote system control via Webex${NC}\n"
    echo ""

    detect_platform
    info "Detected platform: ${OS}-${ARCH}"

    check_sudo
    check_existing
    download_binary

    # Ollama (best-effort)
    ollama_ok=true
    install_ollama || ollama_ok=false
    if [ "$ollama_ok" = true ]; then
        start_ollama || true
        pull_model || true
    fi

    prompt_config
    create_dirs
    generate_env
    generate_config
    install_service || true
    verify
    print_summary
}

main
