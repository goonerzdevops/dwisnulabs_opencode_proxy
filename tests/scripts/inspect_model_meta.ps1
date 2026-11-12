# tests/scripts/inspect_model_meta.ps1
$url = "https://gen.pollinations.ai/v1/models"
$res = & curl.exe -s --max-time 15 $url
$data = $res | ConvertFrom-Json
$target = $data.data | Where-Object { 
    $_.id -like "*openai-fast*" -or 
    $_.id -like "*gpt-oss*" -or 
    $_.aliases -contains "openai-fast" -or 
    $_.aliases -contains "openai" 
}
$target | ConvertTo-Json -Depth 6

