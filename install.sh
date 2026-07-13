#!/usr/bin/env bash
set -euo pipefail

# RemoteClaw Installer — Linux / macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.sh | bash

REPO="3rg0n/remoteclaw"
RELEASE_URL="https://github.com/${REPO}/releases/latest/download"

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
    printf "  Lock down config & secrets? [Y/n] "
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
    $SUDO mkdir -p "$CONF_DIR"
    $SUDO mkdir -p "$LOG_DIR"
    ok "Created ${CONF_DIR} and ${LOG_DIR}"

    if [ "$LOCKDOWN_ENABLED" = true ] && [ "$OS" = "linux" ]; then
        info "Applying lockdown (dedicated low-privilege service user)…"

        # Create a dedicated low-privilege service account. Running as this user
        # (instead of root) is the meaningful privilege reduction: it limits what
        # the agent can touch on the host and addresses the "runs as root" risk.
        $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin remoteclaw 2>/dev/null || true
        ok "Service user 'remoteclaw' ensured."

        # Config/secrets are owned by the service user and readable ONLY by it
        # (0700 dir, 0600 files). The service must read its own config at startup,
        # so it cannot be root-exclusive — but every OTHER account on the box
        # (including other non-root users) is denied. The agent's OWN tools are
        # denied by the in-process lockdown guard (defense-in-depth). Making reads
        # unreadable to the agent's own uid via any command requires the
        # privilege-separated executor tracked in ADR 0004.
        $SUDO chown -R remoteclaw:remoteclaw "$CONF_DIR"
        $SUDO chmod 700 "$CONF_DIR"
        ok "Config directory restricted to the service user."

        # Audit log directory: writable by the service user.
        $SUDO mkdir -p "$LOG_DIR"
        $SUDO chown -R remoteclaw:remoteclaw "$LOG_DIR"
        $SUDO chmod 750 "$LOG_DIR"
        ok "Audit log directory writable by service user."
    elif [ "$LOCKDOWN_ENABLED" = true ]; then
        warn "macOS: dedicated-service-user lockdown requires manual setup; using in-process lockdown only."
    fi
}

# --- Generate .env --------------------------------------------------------
generate_env() {
    if [ "$USE_PASS" = true ]; then
        info "Storing secrets in the current user's pass store…"
        # NOTE: pass is per-user and GPG-session bound. This stores secrets in
        # the INSTALLING user's store. When lockdown runs the service as the
        # dedicated 'remoteclaw' account, that account has its own (empty) store
        # and no access to this one — so the service would fall back to .env.
        # For a service-account + pass setup, the store must belong to the
        # service user (out of scope for this installer; see README/ADR 0003).
        printf '%s' "$BOT_TOKEN" | pass insert -m -f remoteclaw/webex_bot_token >/dev/null 2>&1 || \
            err "Failed to store bot token in pass"
        ok "Secrets stored in pass for user '$(id -un)'."
        if [ "$LOCKDOWN_ENABLED" = true ] && [ "$OS" = "linux" ]; then
            warn "Lockdown runs the service as 'remoteclaw', which cannot read this user's pass store."
            warn "Either provision the store under the service account, or use the .env option instead."
        fi
        # The challenge ciphertext is non-sensitive; keep it in .env for the service.
        if [ -n "$CHALLENGE_ENCRYPTED" ]; then
            echo "CHALLENGE=${CHALLENGE_ENCRYPTED}" | $SUDO tee "$ENV_PATH" >/dev/null
            $SUDO chmod 600 "$ENV_PATH"
            [ "$OS" = "linux" ] && $SUDO chown remoteclaw:remoteclaw "$ENV_PATH" 2>/dev/null || true
        fi
    else
        info "Generating ${ENV_PATH}…"
        local env_content="WEBEX_BOT_TOKEN=${BOT_TOKEN}"
        if [ -n "$CHALLENGE_ENCRYPTED" ]; then
            env_content="${env_content}
CHALLENGE=${CHALLENGE_ENCRYPTED}"
        fi

        echo "$env_content" | $SUDO tee "$ENV_PATH" >/dev/null
        if [ "$LOCKDOWN_ENABLED" = true ]; then
            $SUDO chmod 600 "$ENV_PATH"
            # Owned by the service user so the service can read it; 0600 denies
            # every other account. The agent's own tools are denied by the guard.
            if [ "$OS" = "linux" ]; then
                $SUDO chown remoteclaw:remoteclaw "$ENV_PATH" 2>/dev/null || true
            fi
        else
            $SUDO chmod 644 "$ENV_PATH"
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
    if [ "$LOCKDOWN_ENABLED" = true ]; then
        $SUDO chmod 600 "$CONFIG_PATH"
        # Owned by the service user so the service reads it at startup; 0600
        # denies all other accounts. (The config dir chmod 700 is applied in
        # create_dirs; this re-asserts file ownership after the tee wrote it.)
        if [ "$OS" = "linux" ]; then
            $SUDO chown remoteclaw:remoteclaw "$CONFIG_PATH" 2>/dev/null || true
        fi
    fi
}

# --- Install service ------------------------------------------------------
install_service() {
    info "Installing RemoteClaw as a system service…"
    local install_cmd="$SUDO \"$BIN_PATH\" install --config \"$CONFIG_PATH\""

    if [ "$LOCKDOWN_ENABLED" = true ] && [ "$OS" = "linux" ]; then
        install_cmd="$install_cmd --user remoteclaw"
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

    prompt_config
    create_dirs
    generate_env
    generate_config
    install_service || true
    verify
    print_summary
}

main
