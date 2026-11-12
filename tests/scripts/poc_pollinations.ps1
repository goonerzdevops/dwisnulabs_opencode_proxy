# tests/scripts/poc_pollinations.ps1
# Robust PoC for Pollinations AI POST endpoint using temporary JSON files
$ErrorActionPreference = "Continue"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  POLLINATIONS AI POST OPENAI-COMPATIBLE PoC PROBE" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host ""

$tmpDir = Join-Path $PSScriptRoot "..\..\tests\logs"
if (-not (Test-Path $tmpDir)) { New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null }

# 1. Fetch live models from both endpoints
Write-Host "[1/5] Fetching model catalogs..." -ForegroundColor Yellow
$cat1 = & curl.exe -s --max-time 15 "https://text.pollinations.ai/models"
Write-Host "--- Raw https://text.pollinations.ai/models ---" -ForegroundColor Gray
Write-Host $cat1

$cat2 = & curl.exe -s --max-time 15 "https://gen.pollinations.ai/v1/models"
Write-Host "--- Raw https://gen.pollinations.ai/v1/models (Preview first 300 chars) ---" -ForegroundColor Gray
if ($cat2.Length -gt 300) { Write-Host $cat2.Substring(0, 300) } else { Write-Host $cat2 }

Write-Host ""

# 2. Test POST chat completions with --data-binary to prevent quote stripping
$testModels = @("openai", "qwen-coder", "deepseek", "mistral")
Write-Host "[2/5] Testing POST chat completion with popular models..." -ForegroundColor Yellow

foreach ($m in $testModels) {
    Write-Host "`n--> Testing model: $m" -ForegroundColor Cyan
    $payloadObj = @{
        model = $m
        messages = @(
            @{ role = "user"; content = "Reply with exactly: 'POLLINATIONS $m OK'" }
        )
        temperature = 0.2
    }
    $json = $payloadObj | ConvertTo-Json -Depth 5 -Compress
    $payloadFile = Join-Path $tmpDir "poc_req_$m.json"
    [System.IO.File]::WriteAllText($payloadFile, $json, [System.Text.Encoding]::UTF8)

    $postUrl = "https://text.pollinations.ai/openai/chat/completions"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $resp = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME_TOTAL=%{time_total}s" --max-time 35 -X POST `
        -H "Content-Type: application/json" `
        --data-binary "@$payloadFile" `
        $postUrl

    $sw.Stop()
    Write-Host "Response received in $($sw.ElapsedMilliseconds)ms:" -ForegroundColor Gray
    Write-Host $resp
    Start-Sleep -Seconds 3 # Throttle to respect free rate limit
}

Write-Host ""

# 3. Test Alternative Endpoint https://text.pollinations.ai/ (Direct POST)
Write-Host "[3/5] Testing Direct POST to https://text.pollinations.ai/..." -ForegroundColor Yellow
$directObj = @{
    messages = @(
        @{ role = "user"; content = "Say 'DIRECT POST OK'" }
    )
    model = "openai"
}
$directJson = $directObj | ConvertTo-Json -Depth 5 -Compress
$directFile = Join-Path $tmpDir "poc_direct.json"
[System.IO.File]::WriteAllText($directFile, $directJson, [System.Text.Encoding]::UTF8)

$directResp = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}" --max-time 25 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$directFile" `
    "https://text.pollinations.ai/"

Write-Host "Direct POST Response:" -ForegroundColor Gray
Write-Host $directResp
Start-Sleep -Seconds 3

Write-Host ""

# 4. Test SSE Streaming (stream: true)
Write-Host "[4/5] Testing SSE Streaming (stream: true)..." -ForegroundColor Yellow
$streamObj = @{
    model = "openai"
    messages = @(
        @{ role = "user"; content = "Count 1, 2, 3." }
    )
    stream = $true
}
$streamJson = $streamObj | ConvertTo-Json -Depth 5 -Compress
$streamFile = Join-Path $tmpDir "poc_stream.json"
[System.IO.File]::WriteAllText($streamFile, $streamJson, [System.Text.Encoding]::UTF8)

$streamResp = & curl.exe -s -N --max-time 25 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$streamFile" `
    "https://text.pollinations.ai/openai/chat/completions"

Write-Host "Stream Response Preview:" -ForegroundColor Gray
if ($streamResp.Length -gt 600) {
    Write-Host ($streamResp.Substring(0, 600) + "... [truncated]")
} else {
    Write-Host $streamResp
}
Start-Sleep -Seconds 3

Write-Host ""

# 5. Test Tool Calling
Write-Host "[5/5] Testing Tool Calling Support..." -ForegroundColor Yellow
$toolsObj = @{
    model = "openai"
    messages = @(
        @{ role = "user"; content = "What is the weather in Tokyo? Call get_weather tool." }
    )
    tools = @(
        @{
            type = "function"
            function = @{
                name = "get_weather"
                description = "Get current weather"
                parameters = @{
                    type = "object"
                    properties = @{
                        location = @{ type = "string" }
                    }
                    required = @("location")
                }
            }
        }
    )
    tool_choice = "auto"
}
$toolsJson = $toolsObj | ConvertTo-Json -Depth 10 -Compress
$toolsFile = Join-Path $tmpDir "poc_tools.json"
[System.IO.File]::WriteAllText($toolsFile, $toolsJson, [System.Text.Encoding]::UTF8)

$toolsResp = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}" --max-time 35 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$toolsFile" `
    "https://text.pollinations.ai/openai/chat/completions"

Write-Host "Tool Calling Response:" -ForegroundColor Gray
Write-Host $toolsResp

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  PoC PROBE COMPLETE" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
