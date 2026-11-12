# tests/scripts/test_lanes.ps1
# Runs lane probe and contract header tests
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir '..\..')
Set-Location $ProjectRoot

$LogDir = Join-Path $ProjectRoot 'tests\logs'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
$Log = Join-Path $LogDir 'test_lanes.log'
Remove-Item $Log -ErrorAction SilentlyContinue

$env:OPENCODE_LANE_PROBE = '1'
Write-Host "Running lane probe tests..." -ForegroundColor Cyan
$out = (& go test ./internal/proxy -run 'TestLaneProbe|TestFingerprintHeadersDocumentsContract' -v 2>&1 | Out-String)
Add-Content -Path $Log -Value $out -Encoding UTF8
Write-Host $out
Write-Host "Exit code: $LASTEXITCODE"

