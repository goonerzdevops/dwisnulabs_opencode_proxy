# tests/scripts/test_deepseek.ps1
$payload = @{
    model = "deepseek"
    messages = @(
        @{ role = "user"; content = "Reply with 'DEEPSEEK TEST OK'" }
    )
} | ConvertTo-Json -Compress

$tmpFile = Join-Path $PSScriptRoot "..\..\tests\logs\poc_deepseek_test.json"
[System.IO.File]::WriteAllText($tmpFile, $payload, [System.Text.Encoding]::UTF8)

$res = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}" --max-time 15 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpFile" `
    "https://text.pollinations.ai/openai/chat/completions"

Write-Host "Deepseek Response:"
Write-Host $res

