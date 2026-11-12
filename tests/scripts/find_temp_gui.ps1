$json = Get-Content 'C:/Users/LENOVO/.gemini/antigravity/brain/88805cbc-97c7-4298-a62a-ecfb016f9566/.system_generated/steps/1979/output.txt' -Raw | ConvertFrom-Json
$lines = $json.content -split "\n"

for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -match "attempt_temp_user_creation") {
        Write-Host "Match at line $i : $($lines[$i])"
        $start = [Math]::Max(0, $i - 10)
        $end = [Math]::Min($lines.Count - 1, $i + 35)
        $lines[$start..$end] | ForEach-Object { $_ }
    }
}
