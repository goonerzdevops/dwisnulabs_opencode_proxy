# tests/scripts/test_live_chat_candidates.ps1
$ErrorActionPreference = "Continue"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

Write-Host "================ 1. Testing Kilo Gateway Chat (Keyless) ================" -ForegroundColor Cyan
$kiloPayload = @{
    model = "openrouter/free"
    messages = @(
        @{ role = "user"; content = "Reply with 'KILO FREE OK' only." }
    )
} | ConvertTo-Json -Compress

$tmpKilo = Join-Path $PSScriptRoot "..\..\tests\logs\poc_kilo_test.json"
[System.IO.File]::WriteAllText($tmpKilo, $kiloPayload, $utf8NoBom)

$kiloRes = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 20 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpKilo" `
    "https://api.kilo.ai/api/gateway/chat/completions"

Write-Host "Kilo Chat Response:"
Write-Host $kiloRes

Write-Host ""
Write-Host "================ 2. Testing OVHcloud Chat (Keyless) ================" -ForegroundColor Cyan
$ovhPayload = @{
    model = "Mistral-7B-Instruct-v0.3"
    messages = @(
        @{ role = "user"; content = "Reply with 'OVH FREE OK' only." }
    )
    max_tokens = 32
} | ConvertTo-Json -Compress

$tmpOvh = Join-Path $PSScriptRoot "..\..\tests\logs\poc_ovh_test.json"
[System.IO.File]::WriteAllText($tmpOvh, $ovhPayload, $utf8NoBom)

$ovhRes = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 20 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpOvh" `
    "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/chat/completions"

Write-Host "OVH Chat Response:"
Write-Host $ovhRes

Write-Host ""
Write-Host "================ 3. Testing LLM7 Chat (Key Optional) ================" -ForegroundColor Cyan
$llm7Payload = @{
    model = "fast"
    messages = @(
        @{ role = "user"; content = "Reply with 'LLM7 FREE OK' only." }
    )
    max_tokens = 32
} | ConvertTo-Json -Compress

$tmpLlm7 = Join-Path $PSScriptRoot "..\..\tests\logs\poc_llm7_test.json"
[System.IO.File]::WriteAllText($tmpLlm7, $llm7Payload, $utf8NoBom)

$llm7Res = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME=%{time_total}s" --max-time 20 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpLlm7" `
    "https://api.llm7.io/v1/chat/completions"

Write-Host "LLM7 Chat Response:"
Write-Host $llm7Res

