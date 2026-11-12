$json = Get-Content 'C:/Users/LENOVO/.gemini/antigravity/brain/88805cbc-97c7-4298-a62a-ecfb016f9566/.system_generated/steps/1953/output.txt' -Raw | ConvertFrom-Json
$lines = $json.content -split "\n"
$lines[871..920] | ForEach-Object { $_ }
