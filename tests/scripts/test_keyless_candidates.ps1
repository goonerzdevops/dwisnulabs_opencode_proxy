# tests/scripts/test_keyless_candidates.ps1
$ErrorActionPreference = "Continue"

Write-Host "================ 1. Testing OVHcloud AI Endpoints (Keyless) ================" -ForegroundColor Cyan
# Base: https://oai.endpoints.kepler.ai.cloud.ovh.net/v1
$ovhModels = & curl.exe -s --max-time 15 "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/models"
Write-Host "OVH Models response length: $($ovhModels.Length) chars"
if ($ovhModels.Length -gt 0 -and $ovhModels -notmatch "error|unauthorized|401") {
    Write-Host "OVH models listing accessible! Snippet:"
    $ovhModels.Substring(0, [Math]::Min(300, $ovhModels.Length))
} else {
    Write-Host "OVH models response: $ovhModels"
}

Write-Host ""
Write-Host "================ 2. Testing Kilo Gateway (Keyless) ================" -ForegroundColor Cyan
# Base: https://api.kilo.ai/api/gateway/models
$kiloModels = & curl.exe -s --max-time 15 "https://api.kilo.ai/api/gateway/models"
Write-Host "Kilo Models response length: $($kiloModels.Length) chars"
if ($kiloModels.Length -gt 0 -and $kiloModels -notmatch "error|unauthorized|401") {
    Write-Host "Kilo models listing accessible! Snippet:"
    $kiloModels.Substring(0, [Math]::Min(300, $kiloModels.Length))
} else {
    Write-Host "Kilo models response: $kiloModels"
}

Write-Host ""
Write-Host "================ 3. Testing LLM7 (Key Optional) ================" -ForegroundColor Cyan
# Base: https://api.llm7.io/v1/models
$llm7Models = & curl.exe -s --max-time 15 "https://api.llm7.io/v1/models"
Write-Host "LLM7 Models response length: $($llm7Models.Length) chars"
if ($llm7Models.Length -gt 0 -and $llm7Models -notmatch "error|unauthorized|401") {
    Write-Host "LLM7 models listing accessible! Snippet:"
    $llm7Models.Substring(0, [Math]::Min(300, $llm7Models.Length))
} else {
    Write-Host "LLM7 models response: $llm7Models"
}

