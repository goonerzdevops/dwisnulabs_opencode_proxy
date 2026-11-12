# tests/scripts/probe_env.ps1
# Environment and Go toolchain locator
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir '..\..')
$LogDir = Join-Path $ProjectRoot 'tests\logs'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
$Log = Join-Path $LogDir 'probe_env.log'

$out = @()
$out += "=== Environment Toolchains ==="
$cmd = Get-Command go -ErrorAction SilentlyContinue
if ($cmd) { $out += "PATH_GO=" + $cmd.Source } else { $out += "PATH_GO=none" }
$out += (& go version 2>&1 | Out-String).Trim()
$out | Out-File -FilePath $Log -Encoding UTF8
$out | ForEach-Object { Write-Host $_ }

