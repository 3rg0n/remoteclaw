# RemoteClaw Installer — Windows
# Usage: irm https://raw.githubusercontent.com/3rg0n/remoteclaw/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo        = "3rg0n/remoteclaw"
$ReleaseUrl  = "https://github.com/$Repo/releases/latest/download"

$InstallDir  = "$env:LOCALAPPDATA\RemoteClaw"
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
# On Windows, Scheduled Task registration doesn't always require admin, but we check
# so the installer can create per-user directories and set permissions if needed.

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
    $checksumsUrl = "$ReleaseUrl/CHECKSUMS.txt"

    Write-Info "Downloading $asset from GitHub Releases…"

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $tmpFile = Join-Path $env:TEMP "remoteclaw-download.exe"
    $tmpChecksums = Join-Path $env:TEMP "remoteclaw-CHECKSUMS.txt"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $url -OutFile $tmpFile -UseBasicParsing
        Invoke-WebRequest -Uri $checksumsUrl -OutFile $tmpChecksums -UseBasicParsing
    }
    catch {
        Write-Err "Download failed: $_"
        exit 1
    }

    # Verify checksum
    try {
        $expected = (Get-Content $tmpChecksums | Where-Object { $_ -match $asset }) -replace '\s+.*$',''
        if ($expected) {
            $actual = (Get-FileHash -Path $tmpFile -Algorithm SHA256).Hash.ToLower()
            if ($expected.ToLower() -ne $actual) {
                Write-Err "Checksum verification FAILED for $asset"
                Write-Err "  Expected: $expected"
                Write-Err "  Actual:   $actual"
                Write-Err "The downloaded binary may be corrupted or tampered with."
                exit 1
            }
            Write-Ok "Checksum verified for $asset"
        } else {
            Write-Warn "Could not find checksum for $asset in CHECKSUMS.txt — skipping verification"
        }
    }
    catch {
        Write-Warn "Checksum verification skipped: $_"
    }
    finally {
        Remove-Item $tmpChecksums -Force -ErrorAction SilentlyContinue
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

    # Allowed emails (optional)
    $script:AllowedEmails = Read-Host "  Restrict to allowlisted emails, comma-separated (optional)"

    # Challenge confirmation (optional, default Y)
    $enableChallenge = Read-Host "  Enable destructive-command challenge? [Y/n]"
    $script:ChallengeEncrypted = ""

    if ($enableChallenge -notmatch '^[nN]' -and $enableChallenge -ne "") {
        Write-Host "  Enter challenge passphrase (will be encrypted, then never stored):"
        $passphrase = Read-Host "  Passphrase" -AsSecureString
        $passphraseText = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto([System.Runtime.InteropServices.Marshal]::SecureStringToCoTaskMemUnicode($passphrase))

        if ($passphraseText) {
            Write-Info "Encrypting challenge…"
            try {
                $script:ChallengeEncrypted = & $BinPath encrypt-challenge $passphraseText 2>$null
                if ($script:ChallengeEncrypted) {
                    Write-Ok "Challenge configured."
                }
                else {
                    throw "empty output"
                }
            }
            catch {
                Write-Err "Challenge encryption failed. Proceeding without challenge."
                $script:ChallengeEncrypted = ""
            }
        }
    } else {
        Write-Info "Destructive-command challenge disabled."
    }

    # Store secrets location (pass not typical on Windows; default to .env with note)
    Write-Warn "Secrets will be stored in plaintext .env file (best-effort in-process guard per ADR 0004)."
    Write-Info "For production, consider using an external secret store or running in a VM/container."
    $script:UsePass = $false

    # Lockdown mode (default Y)
    $enableLockdown = Read-Host "  Restrict config/secrets to current user via file ACLs? [Y/n]"
    if ($enableLockdown -notmatch '^[nN]' -and $enableLockdown -ne "") {
        $script:LockdownEnabled = $true
    } else {
        $script:LockdownEnabled = $false
    }
}

# --- Create directories and apply lockdown ---------------------------------

function New-Directories {
    Write-Info "Creating directories…"
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    if (-not (Test-Path $LogDir)) {
        New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
    }
    Write-Ok "Created $InstallDir and $LogDir"

    if ($script:LockdownEnabled) {
        Write-Info "Applying lockdown ACLs to config directory…"

        # RemoteClaw runs in the current user's session (per ADR 0004). Lockdown
        # restricts the directory to the current user + Administrators, disabling
        # inheritance so other local users cannot read config/secrets. The agent's
        # own tools are guarded by the in-process defense-in-depth layer.
        $currentUser = "$env:USERDOMAIN\$env:USERNAME"
        $acl = New-Object System.Security.AccessControl.DirectorySecurity
        $acl.SetAccessRuleProtection($true, $false)  # disable inheritance, drop inherited ACEs

        $userRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            $currentUser, "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow"
        )
        $acl.AddAccessRule($userRule)

        $adminRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            "Administrators", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow"
        )
        $acl.AddAccessRule($adminRule)

        Set-Acl -Path $InstallDir -AclObject $acl
        Write-Ok "Config directory restricted to current user + Administrators."
    }
}

# --- Generate .env ---------------------------------------------------------

function New-EnvFile {
    Write-Info "Generating $EnvPath…"

    $lines = @("WEBEX_BOT_TOKEN=$($script:BotToken)")
    if ($script:ChallengeEncrypted) {
        $lines += "CHALLENGE=$($script:ChallengeEncrypted)"
    }

    $lines -join "`r`n" | Set-Content -Path $EnvPath -Encoding UTF8 -Force

    if ($script:LockdownEnabled) {
        # Restrict .env to current user + Administrators. Inheritance disabled,
        # no entry granted to other users. The agent's own tools are denied by
        # the in-process guard (ADR 0004).
        $currentUser = "$env:USERDOMAIN\$env:USERNAME"
        $acl = New-Object System.Security.AccessControl.FileSecurity
        $acl.SetAccessRuleProtection($true, $false)

        $userRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            $currentUser, "FullControl", "Allow"
        )
        $acl.AddAccessRule($userRule)

        $adminRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            "Administrators", "FullControl", "Allow"
        )
        $acl.AddAccessRule($adminRule)

        Set-Acl $EnvPath $acl
        Write-Ok "Created $EnvPath (current user + Administrators ACL)"
    } else {
        Write-Ok "Created $EnvPath"
    }

    # Note: CHALLENGE is intentionally NOT persisted to a machine environment
    # variable — the challenge ciphertext lives in .env (read by the task).
    # The passphrase is never stored.
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

    $lockdownValue = if ($script:LockdownEnabled) { "true" } else { "false" }

    $logPath = $LogDir -replace '\\', '\\'  # Escape backslashes for YAML

    $configContent = @"
mode: native

webex:
  bot_token: "`${WEBEX_BOT_TOKEN}"
  allowed_emails:$emailsYaml

ai:
  provider: ""
  mode: "interpret"
  model: ""
  temperature: 0.2
  max_tokens: 4096
  max_iterations: 10
  inferd_socket: ""
  openai_base_url: ""
  openai_api_key: "`${OPENAI_API_KEY}"

security:
  dangerous_commands: true
  audit_log: "$logPath\audit"
  rate_limit_per_min: 10
$challengeLine
  lockdown: $lockdownValue
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
"@

    $configContent | Set-Content -Path $ConfigPath -Encoding UTF8 -Force
    Write-Ok "Created $ConfigPath"
}

# --- Install scheduled task ------------------------------------------------

function Install-RemoteClawTask {
    Write-Info "Registering RemoteClaw as a per-user Scheduled Task (runs at logon)…"
    try {
        $taskName = "RemoteClaw"
        $currentUser = "$env:USERDOMAIN\$env:USERNAME"

        # Create trigger: run at logon for the current user
        $trigger = New-ScheduledTaskTrigger -AtLogOn -User $currentUser

        # Create action: run RemoteClaw with the config path
        $action = New-ScheduledTaskAction -Execute $BinPath -Argument "run --config `"$ConfigPath`""

        # Create principal: run as the current user, interactive mode, normal privileges
        $principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Limited

        # Create settings
        $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable

        # Register the task (force overwrite if exists)
        Register-ScheduledTask -TaskName $taskName -Trigger $trigger -Action $action `
            -Principal $principal -Settings $settings -Force | Out-Null

        Write-Ok "Scheduled Task 'RemoteClaw' registered (runs at logon in user session)."
        return $true
    }
    catch {
        Write-Err "Scheduled Task registration failed: $_"
        return $false
    }
}

# --- Verify ----------------------------------------------------------------

function Test-TaskStatus {
    Write-Info "Checking Scheduled Task status…"
    try {
        $task = Get-ScheduledTask -TaskName "RemoteClaw" -ErrorAction SilentlyContinue
        if ($task) {
            Write-Ok "Scheduled Task 'RemoteClaw' is registered."
            Write-Info "  It will start the next time you log on (or manually via 'schtasks /run /tn RemoteClaw')."
        }
        else {
            Write-Warn "Scheduled Task not found. Re-run the installer."
        }
    }
    catch {
        Write-Warn "Could not check task status: $_"
    }
}

# --- Print summary ---------------------------------------------------------

function Write-Summary {
    Write-Host ""
    Write-Host "=== Installation Complete ===" -ForegroundColor White
    Write-Host ""
    Write-Host "  RemoteClaw will run at logon in your user session." -ForegroundColor Green
    Write-Host ""
    Write-Host "  Binary:     $BinPath"
    Write-Host "  Config:     $ConfigPath"
    Write-Host "  Env file:   $EnvPath"
    Write-Host "  Audit logs: $LogDir"
    Write-Host ""
    Write-Host "  Runtime model:"
    Write-Host "    • RemoteClaw starts when you log in (per-user Scheduled Task)."
    Write-Host "    • It runs with your privileges in your session."
    Write-Host "    • Locking your screen keeps it running; logging off stops it."
    Write-Host "    • See ADR 0004 (best-effort defense-in-depth model)."
    Write-Host ""
    Write-Host "  Talk to your bot in Webex — send it a message like:"
    Write-Host '    "What'"'"'s the disk usage?"'
    Write-Host ""
    Write-Host "  Useful commands:"
    Write-Host "    schtasks /run /tn RemoteClaw                            Start RemoteClaw now (manual trigger)"
    Write-Host "    Get-ScheduledTask -TaskName RemoteClaw                  Check task status"
    Write-Host "    Unregister-ScheduledTask -TaskName RemoteClaw -Confirm:`$false   Remove the task"
    Write-Host "    Remove-Item `"$InstallDir`" -Recurse -Force             Remove all files"
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

    Get-UserConfig
    New-EnvFile
    New-ConfigFile
    Install-RemoteClawTask | Out-Null
    Test-TaskStatus
    Write-Summary
}

Main
