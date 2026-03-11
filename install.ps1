# RemoteClaw Installer — Windows
# Usage: irm https://raw.githubusercontent.com/ecopelan/remoteclaw/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo        = "ecopelan/remoteclaw"
$ReleaseUrl  = "https://github.com/$Repo/releases/latest/download"
$OllamaModel = "phi4-mini"

$InstallDir  = "C:\ProgramData\remoteclaw"
$BinPath     = "$InstallDir\remoteclaw.exe"
$ConfigPath  = "$InstallDir\config.yaml"
$EnvPath     = "$InstallDir\.env"
$LogDir      = "$InstallDir\logs"

# --- Helpers ---------------------------------------------------------------

function Write-Info  { param($m) Write-Host "[info]  $m" -ForegroundColor Cyan }
function Write-Ok    { param($m) Write-Host "[ok]    $m" -ForegroundColor Green }
function Write-Warn  { param($m) Write-Host "[warn]  $m" -ForegroundColor Yellow }
function Write-Err   { param($m) Write-Host "[error] $m" -ForegroundColor Red }

# --- Check admin elevation -------------------------------------------------

function Assert-Admin {
    $principal = New-Object Security.Principal.WindowsPrincipal(
        [Security.Principal.WindowsIdentity]::GetCurrent()
    )
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Warn "Not running as Administrator. Re-launching elevated…"
        $scriptUrl = "https://raw.githubusercontent.com/$Repo/main/install.ps1"
        $command = "irm '$scriptUrl' | iex"
        Start-Process powershell.exe -Verb RunAs -ArgumentList "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $command
        exit
    }
}

# --- Detect architecture ---------------------------------------------------

function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "x86"   { return "amd64" }  # 32-bit PS on 64-bit OS still reports x86
        default { Write-Err "Unsupported architecture: $arch"; exit 1 }
    }
}

# --- Check existing install ------------------------------------------------

function Test-ExistingInstall {
    if (Test-Path $BinPath) {
        $ver = & $BinPath version 2>$null
        if (-not $ver) { $ver = "unknown" }
        Write-Warn "RemoteClaw is already installed at $BinPath ($ver)"
        $answer = Read-Host "  Upgrade to latest? [Y/n]"
        if ($answer -match '^[nN]') {
            Write-Info "Aborted."
            exit 0
        }
        Write-Info "Upgrading…"
    }
}

# --- Download binary -------------------------------------------------------

function Install-Binary {
    $arch  = Get-Arch
    $asset = "remoteclaw-windows-${arch}.exe"
    $url   = "$ReleaseUrl/$asset"

    Write-Info "Downloading $asset from GitHub Releases…"

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $tmpFile = Join-Path $env:TEMP "remoteclaw-download.exe"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $url -OutFile $tmpFile -UseBasicParsing
    }
    catch {
        Write-Err "Download failed: $_"
        exit 1
    }

    Copy-Item $tmpFile $BinPath -Force
    Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue

    Write-Ok "Installed remoteclaw → $BinPath"
}

# --- Add to PATH -----------------------------------------------------------

function Add-ToPath {
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    if ($currentPath -notlike "*$InstallDir*") {
        Write-Info "Adding $InstallDir to system PATH…"
        [Environment]::SetEnvironmentVariable(
            "Path",
            "$currentPath;$InstallDir",
            "Machine"
        )
        # Update current session too
        $env:Path = "$env:Path;$InstallDir"
        Write-Ok "Added to PATH."
    }
    else {
        Write-Ok "$InstallDir is already in PATH."
    }
}

# --- Install Ollama --------------------------------------------------------

function Install-Ollama {
    if (Get-Command ollama -ErrorAction SilentlyContinue) {
        Write-Ok "Ollama is already installed."
        return $true
    }

    Write-Info "Ollama not found. Installing…"

    # Try winget first
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        try {
            winget install Ollama.Ollama --accept-source-agreements --accept-package-agreements
            # Refresh PATH for current session
            $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
            if (Get-Command ollama -ErrorAction SilentlyContinue) {
                Write-Ok "Ollama installed via winget."
                return $true
            }
        }
        catch {
            Write-Warn "winget install failed, trying direct download…"
        }
    }

    # Fallback: direct download
    try {
        $ollamaUrl = "https://ollama.com/download/OllamaSetup.exe"
        $ollamaInstaller = Join-Path $env:TEMP "OllamaSetup.exe"
        Invoke-WebRequest -Uri $ollamaUrl -OutFile $ollamaInstaller -UseBasicParsing
        Start-Process -FilePath $ollamaInstaller -ArgumentList "/S" -Wait
        Remove-Item $ollamaInstaller -Force -ErrorAction SilentlyContinue
        # Refresh PATH
        $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
        if (Get-Command ollama -ErrorAction SilentlyContinue) {
            Write-Ok "Ollama installed."
            return $true
        }
    }
    catch {
        # Ignore
    }

    Write-Warn "Ollama installation failed. You can install it manually from https://ollama.com"
    Write-Warn "or configure AWS Bedrock as the AI provider instead."
    return $false
}

# --- Start Ollama ----------------------------------------------------------

function Start-OllamaService {
    # Check if already responding
    try {
        $null = Invoke-WebRequest -Uri "http://localhost:11434/api/version" -UseBasicParsing -TimeoutSec 2
        Write-Ok "Ollama is already running."
        return $true
    }
    catch { }

    Write-Info "Starting Ollama…"

    # Try starting the Ollama service
    $svc = Get-Service -Name "ollama" -ErrorAction SilentlyContinue
    if ($svc) {
        Start-Service -Name "ollama" -ErrorAction SilentlyContinue
    }
    else {
        # Launch ollama serve in the background
        Start-Process ollama -ArgumentList "serve" -WindowStyle Hidden -ErrorAction SilentlyContinue
    }

    # Wait for it to be ready
    for ($i = 0; $i -lt 15; $i++) {
        Start-Sleep -Seconds 1
        try {
            $null = Invoke-WebRequest -Uri "http://localhost:11434/api/version" -UseBasicParsing -TimeoutSec 2
            Write-Ok "Ollama is running."
            return $true
        }
        catch { }
    }

    Write-Warn "Ollama did not start in time. You may need to start it manually."
    return $false
}

# --- Pull model ------------------------------------------------------------

function Pull-OllamaModel {
    if (-not (Get-Command ollama -ErrorAction SilentlyContinue)) {
        return
    }

    Write-Info "Pulling model $OllamaModel… (this may take a few minutes on first run)"
    try {
        & ollama pull $OllamaModel
        Write-Ok "Model $OllamaModel is ready."
    }
    catch {
        Write-Warn "Failed to pull model. You can run 'ollama pull $OllamaModel' later."
    }
}

# --- Interactive prompts ---------------------------------------------------

function Get-UserConfig {
    Write-Host ""
    Write-Host "=== RemoteClaw Configuration ===" -ForegroundColor White

    # Bot token (required)
    while ($true) {
        $script:BotToken = Read-Host "`n  Webex Bot Token (required)"
        if ($script:BotToken) { break }
        Write-Err "Bot token is required. Get one at https://developer.webex.com/my-apps"
    }

    # Challenge passphrase (optional)
    $passphrase = Read-Host "  Challenge passphrase for destructive-command confirmation (optional)"
    $script:ChallengeEncrypted = ""

    if ($passphrase) {
        Write-Info "Encrypting challenge with AES-256-GCM…"
        try {
            $script:ChallengeEncrypted = & $BinPath encrypt-challenge $passphrase 2>$null
            if ($script:ChallengeEncrypted) {
                Write-Ok "Challenge encrypted."
            }
            else {
                throw "empty output"
            }
        }
        catch {
            Write-Warn "Binary encrypt not available. Storing passphrase — encrypt manually before production use."
            $script:ChallengeEncrypted = $passphrase
        }
    }

    # Allowed emails (optional)
    $script:AllowedEmails = Read-Host "  Allowed emails, comma-separated (optional)"
}

# --- Create directories ----------------------------------------------------

function New-Directories {
    Write-Info "Creating directories…"
    if (-not (Test-Path $LogDir)) {
        New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
    }
    Write-Ok "Created $InstallDir and $LogDir"
}

# --- Generate .env ---------------------------------------------------------

function New-EnvFile {
    Write-Info "Generating $EnvPath…"

    $lines = @("WEBEX_BOT_TOKEN=$($script:BotToken)")
    if ($script:ChallengeEncrypted) {
        $lines += "CHALLENGE=$($script:ChallengeEncrypted)"
    }

    $lines -join "`r`n" | Set-Content -Path $EnvPath -Encoding UTF8 -Force

    # Lock down permissions: disable inheritance, grant only current user
    $acl = Get-Acl $EnvPath
    $acl.SetAccessRuleProtection($true, $false)
    # Remove all existing rules
    $acl.Access | ForEach-Object { $acl.RemoveAccessRule($_) } | Out-Null
    # Grant current user full control
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    $rule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        $identity, "FullControl", "Allow"
    )
    $acl.SetAccessRule($rule)
    # Also grant SYSTEM access (needed for service)
    $systemRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        "NT AUTHORITY\SYSTEM", "FullControl", "Allow"
    )
    $acl.SetAccessRule($systemRule)
    Set-Acl $EnvPath $acl

    Write-Ok "Created $EnvPath (restricted ACL)"

    # Also set CHALLENGE as a system environment variable for persistence
    if ($script:ChallengeEncrypted) {
        [Environment]::SetEnvironmentVariable("CHALLENGE", $script:ChallengeEncrypted, "Machine")
        Write-Info "Set CHALLENGE as system environment variable."
    }
}

# --- Generate config.yaml --------------------------------------------------

function New-ConfigFile {
    Write-Info "Generating $ConfigPath…"

    # Build allowed_emails YAML
    $emailsYaml = ""
    if ($script:AllowedEmails) {
        $emails = $script:AllowedEmails -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ }
        foreach ($email in $emails) {
            $emailsYaml += "`n    - `"$email`""
        }
    }
    if (-not $emailsYaml) {
        $emailsYaml = "`n    # - `"admin@company.com`""
    }

    $challengeLine = if ($script:ChallengeEncrypted) {
        '  challenge: "${CHALLENGE}"'
    }
    else {
        '  challenge: ""'
    }

    $configContent = @"
mode: native

webex:
  bot_token: "`${WEBEX_BOT_TOKEN}"
  allowed_emails:$emailsYaml

ai:
  provider: "auto"
  model: "$OllamaModel"
  temperature: 0.2
  max_tokens: 4096
  max_iterations: 10

security:
  dangerous_commands: true
  audit_log: "$LogDir\audit"
  rate_limit_per_min: 10
$challengeLine

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
"@

    $configContent | Set-Content -Path $ConfigPath -Encoding UTF8 -Force
    Write-Ok "Created $ConfigPath"
}

# --- Install service -------------------------------------------------------

function Install-RemoteClawService {
    Write-Info "Installing RemoteClaw as a system service…"
    try {
        & $BinPath install --config $ConfigPath
        Write-Ok "Service installed."
        return $true
    }
    catch {
        Write-Warn "Service installation failed. You can run 'remoteclaw install --config $ConfigPath' manually."
        return $false
    }
}

# --- Verify ----------------------------------------------------------------

function Test-ServiceStatus {
    Write-Info "Checking service status…"
    try {
        & $BinPath status
        Write-Ok "RemoteClaw service is running."
    }
    catch {
        Write-Warn "Service may not be running yet. Check with: remoteclaw status"
    }
}

# --- Print summary ---------------------------------------------------------

function Write-Summary {
    Write-Host ""
    Write-Host "=== Installation Complete ===" -ForegroundColor White
    Write-Host ""
    Write-Host "  Binary:     $BinPath"
    Write-Host "  Config:     $ConfigPath"
    Write-Host "  Env file:   $EnvPath"
    Write-Host "  Audit logs: $LogDir\"
    Write-Host ""
    Write-Host "  Talk to your bot in Webex — send it a message like:"
    Write-Host '    "What'"'"'s the disk usage?"'
    Write-Host ""
    Write-Host "  Useful commands:"
    Write-Host "    remoteclaw status                                       Show service status"
    Write-Host "    remoteclaw uninstall                                    Remove the service"
    Write-Host '    Remove-Item "C:\ProgramData\remoteclaw" -Recurse        Remove all files'
    Write-Host ""
}

# --- Main ------------------------------------------------------------------

function Main {
    Write-Host ""
    Write-Host "RemoteClaw Installer — AI-powered remote system control via Webex" -ForegroundColor White
    Write-Host ""

    Assert-Admin
    Write-Info "Running as Administrator."

    Test-ExistingInstall
    Install-Binary
    Add-ToPath
    New-Directories

    # Ollama (best-effort)
    $ollamaOk = Install-Ollama
    if ($ollamaOk) {
        Start-OllamaService | Out-Null
        Pull-OllamaModel
    }

    Get-UserConfig
    New-EnvFile
    New-ConfigFile
    Install-RemoteClawService | Out-Null
    Test-ServiceStatus
    Write-Summary
}

Main
