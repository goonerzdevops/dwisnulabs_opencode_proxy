# tests/scripts/test_other_free_models.ps1
$ErrorActionPreference = "Continue"

Write-Host "--- 1. Fetching https://text.pollinations.ai/models ---" -ForegroundColor Cyan
$rawModels = & curl.exe -s --max-time 15 "https://text.pollinations.ai/models"
Write-Host "Raw models returned:"
Write-Host $rawModels

$parsed = $rawModels | ConvertFrom-Json -ErrorAction SilentlyContinue
Write-Host "Parsed count: $($parsed.Count)"

Write-Host ""
Write-Host "--- 2. Testing model 'qwen-coder' with fresh IP cooldown (5+ mins idle) ---" -ForegroundColor Cyan
$payload = @{
    model = "qwen-coder"
    messages = @(
        @{ role = "user"; content = "Reply with 'QWEN TEST OK'" }
    )
} | ConvertTo-Json -Compress

$tmpFile = Join-Path $PSScriptRoot "..\..\tests\logs\poc_qwen_test.json"
[System.IO.File]::WriteAllText($tmpFile, $payload, [System.Text.Encoding]::UTF8)

$sw = [System.Diagnostics.Stopwatch]::StartNew()
$res = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 30 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpFile" `
    "https://text.pollinations.ai/openai/chat/completions"
$sw.Stop()

Write-Host "Qwen-coder Response:"
Write-Host $res

