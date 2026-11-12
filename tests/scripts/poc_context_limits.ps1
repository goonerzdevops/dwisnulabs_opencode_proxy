# tests/scripts/poc_context_limits.ps1
# Measures actual input context ingestion and maximum output tokens on Pollinations Free Tier (openai / gpt-oss-20b)
$ErrorActionPreference = "Continue"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  POLLINATIONS AI CONTEXT LENGTH & TOKEN LIMITS PoC" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host ""

$tmpDir = Join-Path $PSScriptRoot "..\..\tests\logs"
if (-not (Test-Path $tmpDir)) { New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null }

# ----------------------------------------------------
# TEST 1: Large Input Context Ingestion Test
# ----------------------------------------------------
Write-Host "[TEST 1/2] Testing Large Input Context Ingestion..." -ForegroundColor Yellow

# Generate a synthetic large input payload (~4,000 words / ~5,500 tokens)
$paragraph = "OpenCode Proxy is an enterprise-grade AI proxy routing requests across OpenCode Zen, Puter, and Pollinations. It provides TLS impersonation, session affinity, rate limit recovery, and proxy pooling. "
$largeContent = ($paragraph * 75) + "`nQuestion: In exactly one short sentence, what is the main purpose of OpenCode Proxy based on the text above?"

Write-Host "Generated input size: $([math]::Round($largeContent.Length / 1024, 2)) KB ($($largeContent.Length) characters)" -ForegroundColor Gray

$inputPayload = @{
    model = "openai"
    messages = @(
        @{ role = "user"; content = $largeContent }
    )
    temperature = 0.1
    max_tokens = 150
} | ConvertTo-Json -Depth 5 -Compress

$inputFile = Join-Path $tmpDir "poc_input_test.json"
[System.IO.File]::WriteAllText($inputFile, $inputPayload, [System.Text.Encoding]::UTF8)

$sw1 = [System.Diagnostics.Stopwatch]::StartNew()
$res1 = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME_TOTAL=%{time_total}s" --max-time 60 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$inputFile" `
    "https://text.pollinations.ai/openai/chat/completions"
$sw1.Stop()

Write-Host "Test 1 Response received in $($sw1.ElapsedMilliseconds)ms:" -ForegroundColor Green
Write-Host $res1

Write-Host ""
Write-Host "Waiting 48 seconds to respect the 45-second per-IP cooldown..." -ForegroundColor DarkYellow
for ($i = 48; $i -gt 0; $i -= 6) {
    Write-Host "  ...cooldown $i seconds remaining..." -ForegroundColor DarkGray
    Start-Sleep -Seconds 6
}
Write-Host "Cooldown complete! Proceeding to Test 2." -ForegroundColor Green
Write-Host ""

# ----------------------------------------------------
# TEST 2: Max Output Generation Test
# ----------------------------------------------------
Write-Host "[TEST 2/2] Testing Maximum Output Generation (max_tokens: 4096)..." -ForegroundColor Yellow

$outputPayload = @{
    model = "openai"
    messages = @(
        @{ role = "user"; content = "Write an extremely comprehensive, exhaustive, and detailed encyclopedic textbook chapter on the history, architecture, and mathematics of modern artificial neural networks from 1950 to 2026. Do not abbreviate or summarize; write as many paragraphs as possible." }
    )
    temperature = 0.7
    max_tokens = 4096
} | ConvertTo-Json -Depth 5 -Compress

$outputFile = Join-Path $tmpDir "poc_output_test.json"
[System.IO.File]::WriteAllText($outputFile, $outputPayload, [System.Text.Encoding]::UTF8)

$sw2 = [System.Diagnostics.Stopwatch]::StartNew()
$res2 = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`nTIME_TOTAL=%{time_total}s" --max-time 90 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$outputFile" `
    "https://text.pollinations.ai/openai/chat/completions"
$sw2.Stop()

Write-Host "Test 2 Response received in $($sw2.ElapsedMilliseconds)ms:" -ForegroundColor Green
Write-Host $res2

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  CONTEXT LIMITS PoC COMPLETE" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

