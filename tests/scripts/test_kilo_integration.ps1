# tests/scripts/test_kilo_integration.ps1
$ErrorActionPreference = "Continue"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

$testPort = 20199
$testDir = Join-Path $PSScriptRoot "..\..\tests\scratch_kilo"
if (Test-Path $testDir) { Remove-Item -Recurse -Force $testDir }
New-Item -ItemType Directory -Path $testDir | Out-Null

$testConfigPath = Join-Path $testDir "config.json"
$testConfig = @{
    port = $testPort
    host = "127.0.0.1"
    api_keys = @("sk-opencode-free")
    permissive_auth = $true
    default_model = "big-pickle"
    upstream_base_url = "https://opencode.ai"
    session_ttl_minutes = 60
    upstream_timeout_seconds = 30
    log_requests = $true
    kilo_enabled = $true
    kilo_default_model = "openrouter/free"
} | ConvertTo-Json -Depth 5

[System.IO.File]::WriteAllText($testConfigPath, $testConfig, $utf8NoBom)

$env:OPENCODE_PROXY_CONFIG = $testConfigPath
$exePath = Join-Path $PSScriptRoot "..\..\bin\opencode-proxy.exe"

Write-Host "Starting opencode-proxy on port $testPort..." -ForegroundColor Cyan
$proc = Start-Process -FilePath $exePath -ArgumentList "-config `"$testConfigPath`"" -PassThru -NoNewWindow
Start-Sleep -Seconds 2

try {
    Write-Host "`n--- Test 1: GET /v2/kilo/models ---" -ForegroundColor Yellow
    $resModels = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}" "http://127.0.0.1:$testPort/v2/kilo/models"
    Write-Host $resModels

    Write-Host "`n--- Test 2: POST /v2/kilo/chat/completions (Non-stream) ---" -ForegroundColor Yellow
    $kiloBody = @{
        model = "openrouter/free"
        messages = @(
            @{ role = "user"; content = "Katakan 'KILO READY' secara singkat." }
        )
        stream = $false
    } | ConvertTo-Json -Compress

    $tmpReq1 = Join-Path $testDir "req_nonstream.json"
    [System.IO.File]::WriteAllText($tmpReq1, $kiloBody, $utf8NoBom)

    $resChat1 = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 25 -X POST `
        -H "Content-Type: application/json" `
        --data-binary "@$tmpReq1" `
        "http://127.0.0.1:$testPort/v2/kilo/chat/completions"
    Write-Host $resChat1

    Write-Host "`n--- Test 3: POST /v1/chat/completions (Auto-Route to Kilo Stream) ---" -ForegroundColor Yellow
    $streamBody = @{
        model = "nvidia/nemotron-3-ultra-550b-a55b:free"
        messages = @(
            @{ role = "user"; content = "Hitung 2+2, jawab angka saja." }
        )
        stream = $true
    } | ConvertTo-Json -Compress

    $tmpReq2 = Join-Path $testDir "req_stream.json"
    [System.IO.File]::WriteAllText($tmpReq2, $streamBody, $utf8NoBom)

    $resChat2 = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 25 -X POST `
        -H "Content-Type: application/json" `
        --data-binary "@$tmpReq2" `
        "http://127.0.0.1:$testPort/v1/chat/completions"
    Write-Host $resChat2

} finally {
    Write-Host "`nStopping server process..." -ForegroundColor Cyan
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force $testDir -ErrorAction SilentlyContinue
}

