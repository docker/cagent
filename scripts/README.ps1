# PowerShell scripts quick reference

# Ensure you run from the repo root where .env resides. Examples:
#   C:\cagent\cagent> scripts\run_groq_tui.ps1 -EnvFile ".env"
#   C:\cagent\cagent> scripts\run_api_groq.ps1 -EnvFile ".env" -Listen ":8080"

Write-Output "PowerShell script helpers available:" 
Write-Output "  - scripts/run_groq_tui.ps1 (-EnvFile .env)"
Write-Output "  - scripts/run_groq_exec.ps1 (-Prompt \"text\")"
Write-Output "  - scripts/run_openrouter_tui.ps1 (-EnvFile .env)"
Write-Output "  - scripts/run_openrouter_exec.ps1 (-Prompt \"text\")"
Write-Output "  - scripts/run_kimi_tui.ps1 (-EnvFile .env)"
Write-Output "  - scripts/run_kimi_exec.ps1 (-Prompt \"text\")"
Write-Output "  - scripts/run_api_groq.ps1 (-EnvFile .env -Listen :8080)"
