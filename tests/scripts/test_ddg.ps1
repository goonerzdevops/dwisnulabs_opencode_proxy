# tests/scripts/test_ddg.ps1
$statusUrl = "https://duckduckgo.com/duckchat/v1/status"
$ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

Write-Host "--- 1. Fetching VQD Token from DuckDuckGo ---" -ForegroundColor Cyan
$headersOutput = & curl.exe -s -i -H "x-vqd-accept: 1" -H "User-Agent: $ua" $statusUrl

$vqdLine = ($headersOutput -split "`r?`n") | Where-Object { $_ -match "^x-vqd-4:\s*(.+)$" }
if (-not $vqdLine) {
    Write-Host "Failed to obtain x-vqd-4 header! Full output:" -ForegroundColor Red
    Write-Host ($headersOutput | Out-String)
    exit 1
}

$vqdToken = $Matches[1].Trim()
Write-Host "Success! Got VQD token: $vqdToken" -ForegroundColor Green

Write-Host ""
Write-Host "--- 2. Sending Chat Request to DuckDuckGo (model: gpt-4o-mini) ---" -ForegroundColor Cyan
$chatUrl = "https://duckduckgo.com/duckchat/v1/chat"
$payload = @{
    model = "gpt-4o-mini"
    messages = @(
        @{ role = "user"; content = "Reply with 'DDG TEST OK' only." }
    )
} | ConvertTo-Json -Compress

$tmpFile = Join-Path $PSScriptRoot "..\..\tests\logs\poc_ddg_test.json"
[System.IO.File]::WriteAllText($tmpFile, $payload, [System.Text.Encoding]::UTF8)

$chatRes = & curl.exe -s -i `
    -H "Content-Type: application/json" `
    -H "User-Agent: $ua" `
    -H "x-vqd-4: $vqdToken" `
    -H "Referer: https://duckduckgo.com/" `
    -H "Origin: https://duckduckgo.com" `
    --data-binary "@$tmpFile" `
    $chatUrl

Write-Host "Chat Response headers & snippet:"
$chatRes -split "`r?`n" | Select-Object -First 30

