$json = Get-Content 'C:/Users/LENOVO/.gemini/antigravity/brain/88805cbc-97c7-4298-a62a-ecfb016f9566/.system_generated/steps/1943/output.txt' -Raw | ConvertFrom-Json
$cleanB64 = $json.content -replace "\s", ""
$bytes = [System.Convert]::FromBase64String($cleanB64)
$text = [System.Text.Encoding]::UTF8.GetString($bytes)
$lines = $text -split "`r?`n"
Write-Host "Total lines: $($lines.Count)"

$matchIdx = -1
for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -match "attempt_temp_user_creation") {
        Write-Host "Match at line $i : $($lines[$i])"
        $matchIdx = $i
    }
}

if ($matchIdx -ge 0) {
    $start = [Math]::Max(0, $matchIdx - 20)
    $end = [Math]::Min($lines.Count - 1, $matchIdx + 40)
    Write-Host "=== Snippet ===" -ForegroundColor Cyan
    $lines[$start..$end] | ForEach-Object { $_ }
}
