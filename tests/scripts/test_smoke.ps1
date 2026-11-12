# tests/scripts/test_smoke.ps1
# Smoke test starting the proxy on a temp config and testing basic HTTP ping
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir '..\..')
Set-Location $ProjectRoot

$LogDir = Join-Path $ProjectRoot 'tests\logs'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
$Log = Join-Path $LogDir 'test_smoke.log'
$SmokeDir = Join-Path $ProjectRoot 'tests\smoke'
$Bin = Join-Path $ProjectRoot 'bin\opencode-proxy.exe'
$Cfg = Join-Path $SmokeDir 'config.json'

function Log([string]$t) { Add-Content -Path $Log -Value $t -Encoding UTF8; Write-Host $t }

Log "=== Building binary ==="
& go build -o $Bin ./cmd/opencode-proxy
if ($LASTEXITCODE -ne 0) { Log "Build failed!"; exit 1 }

$proc = Start-Process -FilePath $Bin -ArgumentList @('-config', $Cfg) -PassThru -RedirectStandardOutput (Join-Path $SmokeDir 'stdout.log') -RedirectStandardError (Join-Path $SmokeDir 'stderr.log')
Log "Proxy started with PID: $($proc.Id)"

$up = $false
for ($i = 0; $i -lt 40; $i++) {
  Start-Sleep -Milliseconds 250
  $code = (& curl.exe -s -o NUL -w '%{http_code}' --max-time 2 'http://127.0.0.1:20144/health')
  if ($code -eq '200') { $up = $true; break }
}

if (-not $up) {
  Log "Proxy did not become ready!"
  Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
  exit 1
}

Log "=== Testing /health ==="
$health = & curl.exe -s --max-time 5 'http://127.0.0.1:20144/health'
Log $health

Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
Log "Smoke test completed successfully."

