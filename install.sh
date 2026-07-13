#!/usr/bin/env bash
set -euo pipefail

# RemoteClaw Installer — Linux / macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.sh | bash [--system] [--user <account>]
#
# Default: per-user installation in ${HOME}/.local/bin and XDG config dirs (no sudo required).
# --system: system-wide service (requires sudo). Optionally --user <account> for the service account.

REPO="3rg0n/remoteclaw"
RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
INSTALL_MODE="user"  # "user" (default) or "system"
SERVICE_USER=""      # Only used in system mode

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

    # Set paths based on install mode
    if [ "$INSTALL_MODE" = "system" ]; then
        # System-wide paths (requires sudo)
        if [ "$OS" = "linux" ]; then
            CONF_DIR="/etc/remoteclaw"
            LOG_DIR="/var/log/remoteclaw"
        else
            CONF_DIR="/usr/local/etc/remoteclaw"
            LOG_DIR="/usr/local/var/log/remoteclaw"
        fi
        BIN_DIR="/usr/local/bin"
    else
        # Per-user paths (no sudo required; XDG-compliant)
        BIN_DIR="${HOME}/.local/bin"
        CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/remoteclaw"
        LOG_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/remoteclaw/logs"
    fi

    BIN_PATH="${BIN_DIR}/remoteclaw"
    CONFIG_PATH="${CONF_DIR}/config.yaml"
    ENV_PATH="${CONF_DIR}/.env"
}

# --- Check sudo (only for system mode) ---------------------------------
check_sudo() {
    if [ "$INSTALL_MODE" = "user" ]; then
        # Per-user install: no sudo required
        SUDO=""
        return 0
    fi

    # System mode: check for sudo
    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    elif command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        info "Sudo access is required for system-wide installation."
        sudo -v || { err "Failed to obtain sudo. Run as root or ensure sudo is configured."; exit 1; }
    else
        err "System-wide installation requires root privileges. Please run as root or with sudo."
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

# --- Encrypt challenge passphrase -----------------------------------------------
encrypt_challenge() {
    local passphrase="$1"
    # Call the binary to encrypt the challenge using AES-256-GCM
    if "$BIN_PATH" encrypt-challenge "$passphrase" 2>/dev/null; then
        return 0
    else
        # Binary failed or not yet installed
        return 1
    fi
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

    # Allowed emails (optional)
    printf "  Restrict to allowlisted emails, comma-separated (optional): "
    read -r ALLOWED_EMAILS_RAW
    ALLOWED_EMAILS_RAW="${ALLOWED_EMAILS_RAW:-}"

    # Challenge confirmation (optional, default Y)
    printf "  Enable destructive-command challenge? [Y/n] "
    read -r ENABLE_CHALLENGE
    ENABLE_CHALLENGE="${ENABLE_CHALLENGE:-y}"

    CHALLENGE_ENCRYPTED=""
    if [[ ! "$ENABLE_CHALLENGE" =~ ^[nN] ]]; then
        printf "  Challenge passphrase (will be encrypted): "
        read -rs CHALLENGE_PASSPHRASE
        echo ""
        if [ -n "$CHALLENGE_PASSPHRASE" ]; then
            info "Encrypting challenge…"
            if CHALLENGE_ENCRYPTED=$(encrypt_challenge "$CHALLENGE_PASSPHRASE"); then
                ok "Challenge configured."
            else
                err "Challenge encryption failed. Proceeding without challenge."
                CHALLENGE_ENCRYPTED=""
            fi
        fi
    fi

    # Store secrets with pass or .env (default Y if pass available, else N)
    USE_PASS=false
    if command -v pass >/dev/null 2>&1 && [ -d "${HOME}/.password-store" ]; then
        printf "  Store secrets in pass? [Y/n] "
        read -r USE_PASS_ANSWER
        USE_PASS_ANSWER="${USE_PASS_ANSWER:-y}"
        if [[ ! "$USE_PASS_ANSWER" =~ ^[nN] ]]; then
            USE_PASS=true
        fi
    fi

    if [ "$USE_PASS" = false ]; then
        warn "Secrets will be stored in plaintext .env file. For production, consider using the pass secret store."
    fi

    # Lockdown mode (default Y)
    if [ "$INSTALL_MODE" = "system" ]; then
        printf "  Enable in-process secrets guard? [Y/n] "
    else
        printf "  Enable config/secrets lockdown? [Y/n] "
    fi
    read -r ENABLE_LOCKDOWN
    ENABLE_LOCKDOWN="${ENABLE_LOCKDOWN:-y}"
    if [[ ! "$ENABLE_LOCKDOWN" =~ ^[nN] ]]; then
        LOCKDOWN_ENABLED=true
    else
        LOCKDOWN_ENABLED=false
    fi
}

# --- Create directories and apply lockdown ------------------------------------
create_dirs() {
    info "Creating directories…"

    if [ "$INSTALL_MODE" = "user" ]; then
        # Per-user: create without sudo
        mkdir -p "$CONF_DIR"
        mkdir -p "$LOG_DIR"
        ok "Created ${CONF_DIR} and ${LOG_DIR}"

        # Per-user dirs: restrict to user only for security
        chmod 700 "$CONF_DIR"
        chmod 700 "$LOG_DIR"
    else
        # System mode: create with sudo, optionally dedicated user
        $SUDO mkdir -p "$CONF_DIR"
        $SUDO mkdir -p "$LOG_DIR"
        ok "Created ${CONF_DIR} and ${LOG_DIR}"

        if [ -n "$SERVICE_USER" ] && [ "$OS" = "linux" ]; then
            info "Configuring system service user…"
            # Create a dedicated low-privilege service account
            $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER" 2>/dev/null || true
            ok "Service user '${SERVICE_USER}' ensured."

            # Config/secrets owned by the service user, readable only by it (0700 dir, 0600 files).
            # The service reads its own config at startup; other accounts are denied.
            # In-process guard provides defense-in-depth (ADR 0004).
            $SUDO chown -R "${SERVICE_USER}:${SERVICE_USER}" "$CONF_DIR"
            $SUDO chmod 700 "$CONF_DIR"
            ok "Config directory restricted to service user '${SERVICE_USER}'."

            # Audit log directory: writable by service user
            $SUDO chown -R "${SERVICE_USER}:${SERVICE_USER}" "$LOG_DIR"
            $SUDO chmod 750 "$LOG_DIR"
            ok "Audit log directory writable by service user."
        fi
    fi
}

# --- Generate .env --------------------------------------------------------
generate_env() {
    if [ "$USE_PASS" = true ]; then
        info "Storing secrets in the current user's pass store…"
        # pass is per-user and GPG-session bound. This stores secrets in
        # the installing user's store. For a dedicated service account with pass,
        # the store must belong to that account (out of scope; see ADR 0003).
        printf '%s' "$BOT_TOKEN" | pass insert -m -f remoteclaw/webex_bot_token >/dev/null 2>&1 || \
            err "Failed to store bot token in pass"
        ok "Secrets stored in pass for user '$(id -un)'."
        if [ "$INSTALL_MODE" = "system" ] && [ -n "$SERVICE_USER" ]; then
            warn "System service runs as '${SERVICE_USER}', which cannot read this user's pass store."
            warn "Either provision the store under that account, or use the .env option instead."
        fi
        # The challenge ciphertext is non-sensitive; keep it in .env for the service.
        if [ -n "$CHALLENGE_ENCRYPTED" ]; then
            echo "CHALLENGE=${CHALLENGE_ENCRYPTED}" | $SUDO tee "$ENV_PATH" >/dev/null
            $SUDO chmod 600 "$ENV_PATH"
            if [ "$INSTALL_MODE" = "system" ] && [ -n "$SERVICE_USER" ] && [ "$OS" = "linux" ]; then
                $SUDO chown "${SERVICE_USER}:${SERVICE_USER}" "$ENV_PATH" 2>/dev/null || true
            fi
        fi
    else
        info "Generating ${ENV_PATH}…"
        local env_content="WEBEX_BOT_TOKEN=${BOT_TOKEN}"
        if [ -n "$CHALLENGE_ENCRYPTED" ]; then
            env_content="${env_content}
CHALLENGE=${CHALLENGE_ENCRYPTED}"
        fi

        echo "$env_content" | $SUDO tee "$ENV_PATH" >/dev/null
        $SUDO chmod 600 "$ENV_PATH"

        # Ownership depends on install mode
        if [ "$INSTALL_MODE" = "system" ] && [ -n "$SERVICE_USER" ] && [ "$OS" = "linux" ]; then
            # System mode: owned by service user
            $SUDO chown "${SERVICE_USER}:${SERVICE_USER}" "$ENV_PATH" 2>/dev/null || true
        fi
        ok "Created ${ENV_PATH}"
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

    local lockdown_value="true"
    if [ "$LOCKDOWN_ENABLED" = false ]; then
        lockdown_value="false"
    fi

    $SUDO tee "$CONFIG_PATH" >/dev/null <<YAML
mode: native

webex:
  bot_token: "\${WEBEX_BOT_TOKEN}"
  allowed_emails:${emails_yaml}

ai:
  provider: ""
  mode: "interpret"
  model: ""
  temperature: 0.2
  max_tokens: 4096
  max_iterations: 10
  inferd_socket: ""
  openai_base_url: ""
  openai_api_key: "\${OPENAI_API_KEY}"

security:
  dangerous_commands: true
  audit_log: "${LOG_DIR}/audit"
  rate_limit_per_min: 10
${challenge_line}
  lockdown: ${lockdown_value}
  protected_paths: []

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
    $SUDO chmod 600 "$CONFIG_PATH"

    # Ownership depends on install mode
    if [ "$INSTALL_MODE" = "system" ] && [ -n "$SERVICE_USER" ] && [ "$OS" = "linux" ]; then
        # System mode: owned by service user so it can read at startup
        $SUDO chown "${SERVICE_USER}:${SERVICE_USER}" "$CONFIG_PATH" 2>/dev/null || true
    fi
}

# --- Install service ------------------------------------------------------
install_service() {
    if [ "$INSTALL_MODE" = "user" ]; then
        info "Installing RemoteClaw as a user service…"
    else
        info "Installing RemoteClaw as a system service…"
    fi

    local install_cmd="$SUDO \"$BIN_PATH\" install --config \"$CONFIG_PATH\""

    if [ "$INSTALL_MODE" = "system" ] && [ -n "$SERVICE_USER" ]; then
        install_cmd="$install_cmd --system --user \"$SERVICE_USER\""
    elif [ "$INSTALL_MODE" = "system" ]; then
        install_cmd="$install_cmd --system"
    fi

    if eval "$install_cmd"; then
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
    if [ "$INSTALL_MODE" = "user" ]; then
        echo "  Mode:       Per-user installation"
        echo "  Binary:     ${BIN_PATH}"
        echo "  Config:     ${CONFIG_PATH}"
        echo "  Env file:   ${ENV_PATH}"
        echo "  Audit logs: ${LOG_DIR}/"
        echo ""
        if [[ ":$PATH:" != *":${HOME}/.local/bin:"* ]]; then
            warn "Note: ${HOME}/.local/bin is not in your PATH. Add it or use the full path to run remoteclaw."
        fi
        echo "  Talk to your bot in Webex — send it a message like:"
        echo "    \"What's the disk usage?\""
        echo ""
        echo "  Useful commands:"
        echo "    remoteclaw status                   Show service status"
        echo "    remoteclaw uninstall                Remove the service"
        echo "    rm ${BIN_PATH}                      Remove the binary"
        echo "    rm -rf ${CONF_DIR}                  Remove config"
    else
        echo "  Mode:       System-wide installation"
        echo "  Binary:     ${BIN_PATH}"
        echo "  Config:     ${CONFIG_PATH}"
        echo "  Env file:   ${ENV_PATH}"
        echo "  Audit logs: ${LOG_DIR}/"
        echo ""
        echo "  Talk to your bot in Webex — send it a message like:"
        echo "    \"What's the disk usage?\""
        echo ""
        echo "  Useful commands:"
        echo "    sudo remoteclaw status --system       Show service status"
        echo "    sudo remoteclaw uninstall --system    Remove the service"
        echo "    sudo rm ${BIN_PATH}                   Remove the binary"
        echo "    sudo rm -rf ${CONF_DIR}               Remove config"
    fi
    echo ""
}

# --- Parse command-line arguments ------------------------------------------
parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --system)
                INSTALL_MODE="system"
                shift
                ;;
            --user)
                if [ $# -lt 2 ]; then
                    err "--user requires an account name"
                    exit 1
                fi
                SERVICE_USER="$2"
                shift 2
                ;;
            *)
                err "Unknown argument: $1"
                exit 1
                ;;
        esac
    done
}

# --- Main ------------------------------------------------------------------
main() {
    parse_args "$@"

    echo ""
    printf "${BOLD}RemoteClaw Installer — AI-powered remote system control via Webex${NC}\n"
    echo ""

    detect_platform
    info "Detected platform: ${OS}-${ARCH}"
    if [ "$INSTALL_MODE" = "user" ]; then
        info "Install mode: per-user (no sudo required)"
    else
        info "Install mode: system-wide"
        if [ -n "$SERVICE_USER" ]; then
            info "Service account: ${SERVICE_USER}"
        fi
    fi

    check_sudo
    check_existing
    download_binary

    prompt_config
    create_dirs
    generate_env
    generate_config
    install_service || true
    verify
    print_summary
}

main "$@"
