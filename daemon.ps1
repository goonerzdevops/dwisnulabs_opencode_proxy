<#
.SYNOPSIS
    OpenCode Proxy Daemon Manager for Windows (systemctl equivalent)
.DESCRIPTION
    Manages opencode-proxy background execution, autostart on Windows logon,
    health inspection, and live logging.
.EXAMPLE
    .\daemon.ps1 start
    .\daemon.ps1 status
    .\daemon.ps1 logs
    .\daemon.ps1 stop
    .\daemon.ps1 restart
    .\daemon.ps1 install    (Autostart on Windows logon)
    .\daemon.ps1 uninstall  (Remove Windows autostart)
#>

[CmdletBinding()]
param (
    [Parameter(Position = 0)]
    [ValidateSet('start', 'stop', 'restart', 'status', 'logs', 'install', 'uninstall', 'help', 'run')]
    [string]$Action = 'status',

    [Parameter()]
    [switch]$Follow
)

$ErrorActionPreference = 'Stop'
$ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$BinPath = Join-Path $ScriptRoot 'bin\opencode-proxy.exe'
$LogDir = Join-Path $ScriptRoot 'logs'
$LogOut = Join-Path $LogDir 'opencode-proxy.log'
$LogErr = Join-Path $LogDir 'opencode-proxy.err.log'
$TaskName = 'OpenCodeProxyDaemon'
$Port = 20144

if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}

function Get-ProxyProcess {
    Get-Process -Name 'opencode-proxy*' -ErrorAction SilentlyContinue | Where-Object {
        try {
            if ($_.Path) {
                return ($_.Path.ToLower() -eq $BinPath.ToLower() -or $_.Path.ToLower().Contains('opencode_proxy'))
            }
        } catch {
            return $true
        }
        return $true
    }
}

function Test-PortListening {
    param([int]$TargetPort)
    $conn = Get-NetTCPConnection -LocalPort $TargetPort -State Listen -ErrorAction SilentlyContinue
    return [bool]$conn
}

function Show-Status {
    Write-Host ""
    Write-Host "=== [ OpenCode Proxy Daemon Status ] ===" -ForegroundColor Cyan
    $procs = Get-ProxyProcess
    $isListening = Test-PortListening -TargetPort $Port

    if ($procs) {
        foreach ($p in $procs) {
            $wsMB = [math]::Round($p.WorkingSet64 / 1MB, 2)
            $startTime = $p.StartTime.ToString('yyyy-MM-dd HH:mm:ss')
            Write-Host "[RUNNING] Process:        PID $($p.Id), Memory: $wsMB MB" -ForegroundColor Green
            Write-Host "          Started:        $startTime" -ForegroundColor Gray
            Write-Host "          Binary:         $($p.Path)" -ForegroundColor Gray
        }
    } else {
        Write-Host "[STOPPED] Process:        No active process found" -ForegroundColor Yellow
    }

    if ($isListening) {
        Write-Host "[ACTIVE]  Port $Port (TCP)     LISTENING (http://127.0.0.1:$Port)" -ForegroundColor Green
    } else {
        Write-Host "[CLOSED]  Port $Port (TCP)     CLOSED" -ForegroundColor Yellow
    }

    # Query health
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 2 -ErrorAction Stop
        Write-Host "[ONLINE]  API Status:     HEALTHY" -ForegroundColor Green
        Write-Host "          Default Model:  $($health.default_model)" -ForegroundColor White
        Write-Host "          Total Requests: $($health.requests)" -ForegroundColor White
        Write-Host "          Active Sessions:$($health.sessions)" -ForegroundColor White
        Write-Host "          Uptime:         $($health.uptime_seconds) seconds" -ForegroundColor White
        if ($health.proxies) {
            $poolList = @()
            foreach ($item in $health.proxies) {
                $st = if ($item.healthy) { "Ready" } else { "Cooldown" }
                $poolList += "$($item.url) [$st]"
            }
            Write-Host "          Proxy Pool:     $($poolList -join ', ')" -ForegroundColor Cyan
        }
    } catch {
        if ($procs) {
            Write-Host "[WAIT]    API Status:     Starting up ($($_.Exception.Message))" -ForegroundColor DarkYellow
        }
    }

    # Check Autostart Shortcut
    $startupLnk = Join-Path ([Environment]::GetFolderPath('Startup')) 'OpenCodeProxy.lnk'
    if (Test-Path $startupLnk) {
        Write-Host "[ENABLED] Windows Autostart: ENABLED (Startup Folder)" -ForegroundColor Green
    } else {
        Write-Host "[OFF]     Windows Autostart: DISABLED (Run '.\daemon.ps1 install' to enable)" -ForegroundColor Gray
    }
    Write-Host ""
}

function Start-Daemon {
    $procs = Get-ProxyProcess
    if ($procs) {
        Write-Host "Daemon is already running with PID: $($procs[0].Id)" -ForegroundColor Yellow
        Show-Status
        return
    }

    if (-not (Test-Path $BinPath)) {
        Write-Host "Binary not found at $BinPath. Building it now..." -ForegroundColor Yellow
        & go build -o $BinPath ./cmd/opencode-proxy
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Build failed! Aborting." -ForegroundColor Red
            return
        }
    }

    Write-Host "Starting opencode-proxy in background..." -ForegroundColor Cyan

    $p = Start-Process -FilePath $BinPath `
                       -ArgumentList "-config `"$ScriptRoot\config.json`"" `
                       -WorkingDirectory $ScriptRoot `
                       -WindowStyle Hidden `
                       -RedirectStandardOutput $LogOut `
                       -RedirectStandardError $LogErr `
                       -PassThru

    $online = $false
    for ($i = 0; $i -lt 15; $i++) {
        Start-Sleep -Milliseconds 200
        if (Test-PortListening -TargetPort $Port) {
            $online = $true
            break
        }
    }

    if ($online) {
        Write-Host "[OK] Daemon started successfully (PID: $($p.Id), Port: $Port)" -ForegroundColor Green
        Write-Host "     Logs:      $LogOut" -ForegroundColor Gray
        Write-Host "     Dashboard: http://127.0.0.1:$Port/" -ForegroundColor Cyan
    } else {
        Write-Host "[WARN] Daemon started (PID: $($p.Id)), but port $Port not ready yet." -ForegroundColor Yellow
        Write-Host "       Check logs: .\daemon.ps1 logs" -ForegroundColor Gray
    }
}

function Stop-Daemon {
    $procs = Get-ProxyProcess
    if (-not $procs) {
        Write-Host "No running opencode-proxy process found." -ForegroundColor Yellow
        return
    }

    foreach ($p in $procs) {
        Write-Host "Stopping opencode-proxy (PID: $($p.Id))..." -ForegroundColor Yellow
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Milliseconds 500
    Write-Host "[OK] Daemon stopped." -ForegroundColor Green
}

function View-Logs {
    $targetLog = if (Test-Path $LogOut) { $LogOut } elseif (Test-Path $LogErr) { $LogErr } else { $null }
    if (-not $targetLog) {
        Write-Host "Log file does not exist yet: $LogOut" -ForegroundColor Yellow
        return
    }

    Write-Host "Showing $targetLog (Press Ctrl+C to exit)..." -ForegroundColor Cyan
    if ($Follow) {
        Get-Content -Path $targetLog -Tail 30 -Wait
    } else {
        Get-Content -Path $targetLog -Tail 30
        Write-Host "`n[Tip: Run '.\daemon.ps1 logs -Follow' to stream logs in real time]" -ForegroundColor Gray
    }
}

function Install-Autostart {
    Write-Host "Setting up Windows autostart (runs silently in background on Windows login)..." -ForegroundColor Cyan

    $startupFolder = [Environment]::GetFolderPath('Startup')
    $shortcutPath = Join-Path $startupFolder 'OpenCodeProxy.lnk'
    $vbsPath = Join-Path $ScriptRoot 'run-hidden.vbs'

    $vbsContent = "Set WshShell = CreateObject(`"WScript.Shell`")`r`nWshShell.CurrentDirectory = `"$ScriptRoot`"`r`nWshShell.Run `"cmd.exe /c bin\opencode-proxy.exe -config `"$ScriptRoot\config.json`" >> logs\opencode-proxy.log 2>&1`", 0, False"
    Set-Content -Path $vbsPath -Value $vbsContent -Encoding ASCII

    $wsh = New-Object -ComObject WScript.Shell
    $shortcut = $wsh.CreateShortcut($shortcutPath)
    $shortcut.TargetPath = "wscript.exe"
    $shortcut.Arguments = "`"$vbsPath`""
    $shortcut.WorkingDirectory = $ScriptRoot
    $shortcut.Description = "OpenCode Proxy Background Daemon"
    $shortcut.Save()

    Write-Host "[OK] Autostart successfully registered in Windows Startup folder!" -ForegroundColor Green
    Write-Host "     Shortcut: $shortcutPath" -ForegroundColor White
    Write-Host "     Proxy will now start silently in background whenever you log on to Windows." -ForegroundColor Cyan
}

function Uninstall-Autostart {
    $startupFolder = [Environment]::GetFolderPath('Startup')
    $shortcutPath = Join-Path $startupFolder 'OpenCodeProxy.lnk'
    if (Test-Path $shortcutPath) {
        Remove-Item $shortcutPath -Force
        Write-Host "[OK] Autostart shortcut removed from Windows Startup folder." -ForegroundColor Green
    } else {
        Write-Host "Autostart shortcut is not present in Startup folder." -ForegroundColor Yellow
    }
}

function Run-Foreground {
    Write-Host ""
    Write-Host "=== [ OpenCode Proxy (Foreground Mode) ] ===" -ForegroundColor Cyan
    Write-Host "Running directly in terminal. Press Ctrl+C to stop." -ForegroundColor Gray
    Write-Host ""

    $procs = Get-ProxyProcess
    if ($procs) {
        Write-Host "[WARN] An instance of opencode-proxy is already running (PID: $($procs[0].Id))." -ForegroundColor Yellow
        Write-Host "       Stop it first ('daemon.bat stop') before running in foreground." -ForegroundColor Yellow
        return
    }

    if (-not (Test-Path $BinPath)) {
        Write-Host "Binary not found at $BinPath. Building it now..." -ForegroundColor Yellow
        & go build -o $BinPath ./cmd/opencode-proxy
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Build failed! Aborting." -ForegroundColor Red
            return
        }
    }

    & $BinPath -config "$ScriptRoot\config.json"
}

switch ($Action) {
    'start'     { Start-Daemon }
    'stop'      { Stop-Daemon }
    'restart'   { Stop-Daemon; Start-Daemon }
    'status'    { Show-Status }
    'logs'      { View-Logs }
    'install'   { Install-Autostart }
    'uninstall' { Uninstall-Autostart }
    'run'       { Run-Foreground }
    default     {
        Write-Host "Usage: .\daemon.ps1 {start|stop|restart|status|logs|install|uninstall|run}" -ForegroundColor Cyan
    }
}
