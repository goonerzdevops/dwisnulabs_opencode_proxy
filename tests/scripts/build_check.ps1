# tests/scripts/build_check.ps1
# Build, vet, and unit test verification
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir '..\..')
Set-Location $ProjectRoot

$LogDir = Join-Path $ProjectRoot 'tests\logs'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
$Log = Join-Path $LogDir 'build_check.log'
Remove-Item $Log -ErrorAction SilentlyContinue

function Log([string]$text) { Add-Content -Path $Log -Value $text -Encoding UTF8; Write-Host $text }
function Run([string]$label, [string[]]$argv) {
  Log "=== $label ==="
  $output = & go @argv 2>&1 | Out-String
  Log $output
  Log "$label exit=$LASTEXITCODE"
}

Log ("Go Version: " + (& go version 2>&1 | Out-String).Trim())
Run 'go mod tidy' @('mod', 'tidy')
Run 'go vet ./...' @('vet', './...')
Run 'go build ./...' @('build', './...')
Run 'go test ./...' @('test', './...')

