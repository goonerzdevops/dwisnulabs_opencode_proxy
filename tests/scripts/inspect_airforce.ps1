$json = Get-Content 'C:/Users/LENOVO/.gemini/antigravity/brain/88805cbc-97c7-4298-a62a-ecfb016f9566/.system_generated/steps/1883/output.txt' -Raw | ConvertFrom-Json
Write-Host "Total models: $($json.data.Count)"

$freeOperational = $json.data | Where-Object { 
    ($_.tier -eq 'free' -or ($_.access_tiers -contains 'free')) -and $_.status -eq 'operational' -and $_.supports_chat -eq $true
}

Write-Host "Operational Free Chat Models: $($freeOperational.Count)"
$freeOperational | Select-Object id, owned_by, status, max_tokens | Format-Table -AutoSize

