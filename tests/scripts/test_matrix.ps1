# tests/scripts/test_matrix.ps1
# Model matrix prompt probe across models
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir '..\..')
Set-Location $ProjectRoot

$LogDir = Join-Path $ProjectRoot 'tests\logs'
$SmokeDir = Join-Path $ProjectRoot 'tests\smoke'
$Log = Join-Path $LogDir 'test_matrix.log'
Remove-Item $Log -ErrorAction SilentlyContinue

function Log([string]$t) { Add-Content -Path $Log -Value $t -Encoding UTF8; Write-Host $t }

$models = @('deepseek-chat', 'claude-3-5-haiku', 'gpt-4o-mini')
foreach ($m in $models) {
  $payload = (@{ model = $m; messages = @(@{ role = 'user'; content = 'ping' }) } | ConvertTo-Json -Compress)
  $out = (& curl.exe -s -w "`nHTTP=%{http_code}" --max-time 30 -H 'Content-Type: application/json' -H 'Authorization: Bearer test' --data $payload 'http://127.0.0.1:20144/v2/chat/completions')
  Log "--- model=$m ---"
  Log $out
}

