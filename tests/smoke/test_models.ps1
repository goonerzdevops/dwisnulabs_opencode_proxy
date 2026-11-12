$models = @('beam-free', 'glm-5.3-flash-free', 'mimo-v2.6-flash-free', 'nemotron-3.5-lightning-free', 'jev-1.13-free', 'flint-40b-free')
foreach ($m in $models) {
    $body = @{
        model = $m
        stream = $false
        messages = @(@{ role = "user"; content = "Say pong" })
    } | ConvertTo-Json -Compress

    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:20144/v1/chat/completions" -Method Post -Headers @{ "Authorization" = "Bearer sk-opencode-free" } -ContentType "application/json" -Body $body
        Write-Host "Model $m => SUCCESS: $($res.choices[0].message.content)"
    } catch {
        Write-Host "Model $m => ERROR: $($_.ErrorDetails.Message)"
    }
}
