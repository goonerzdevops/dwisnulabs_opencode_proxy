# tests/scripts/test_puter_anon.ps1
$ErrorActionPreference = "Continue"

$payload = @{
    is_temp = $true
} | ConvertTo-Json -Compress

$tmpFile = Join-Path $PSScriptRoot "..\..\tests\logs\poc_puter_anon.json"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($tmpFile, $payload, $utf8NoBom)

Write-Host "--- Testing POST https://puter.com/signup with is_temp=true ---" -ForegroundColor Cyan
$res1 = & curl.exe -s -i --max-time 15 -X POST `
    -H "Content-Type: application/json" `
    -H "Origin: https://puter.com" `
    -H "Referer: https://puter.com/" `
    -H "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36" `
    --data-binary "@$tmpFile" `
    "https://puter.com/signup"

Write-Host "Puter.com /signup response:"
Write-Host ($res1 | Out-String)

Write-Host ""
Write-Host "--- Testing POST https://api.puter.com/signup with is_temp=true ---" -ForegroundColor Cyan
$res2 = & curl.exe -s -i --max-time 15 -X POST `
    -H "Content-Type: application/json" `
    -H "Origin: https://puter.com" `
    -H "Referer: https://puter.com/" `
    -H "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36" `
    --data-binary "@$tmpFile" `
    "https://api.puter.com/signup"

Write-Host "api.puter.com /signup response:"
Write-Host ($res2 | Out-String)

