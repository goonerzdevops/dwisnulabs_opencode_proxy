# tests/scripts/test_airforce_chat.ps1
$modelsRaw = & curl.exe -s --max-time 15 "https://api.airforce/v1/models"
$modelsJson = $modelsRaw | ConvertFrom-Json

Write-Host "Total models from api.airforce: $($modelsJson.data.Count)"

$freeModels = $modelsJson.data | Where-Object { 
    $_.tier -eq 'free' -and $_.supports_chat -eq $true -and $_.status -eq 'operational'
}

Write-Host "Operational Free Chat models ($($freeModels.Count)):"
$freeModels | Select-Object id, owned_by, status | Format-Table -AutoSize

# Test sending chat completion to the first free model without API key
$testModel = $freeModels[0].id
if (-not $testModel) {
    $testModel = "kimi-k2.7-code"
}

Write-Host "--- Testing Chat Completion on model '$testModel' WITHOUT API key ---" -ForegroundColor Cyan
$payload = @{
    model = $testModel
    messages = @(
        @{ role = "user"; content = "Reply with 'AIRFORCE TEST OK' only." }
    )
} | ConvertTo-Json -Compress

$tmpFile = Join-Path $PSScriptRoot "..\..\tests\logs\poc_airforce_test.json"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($tmpFile, $payload, $utf8NoBom)

$chatRes = & curl.exe -s -w "`nHTTP_STATUS=%{http_code}`n" --max-time 20 -X POST `
    -H "Content-Type: application/json" `
    --data-binary "@$tmpFile" `
    "https://api.airforce/v1/chat/completions"

Write-Host "Chat Response:"
Write-Host $chatRes
