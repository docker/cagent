$ErrorActionPreference = 'Stop'

# Determine the PowerShell profile path
$profilePath = $PROFILE.CurrentUserAllHosts
$profileDir = Split-Path -Parent $profilePath
if (!(Test-Path $profileDir)) { New-Item -ItemType Directory -Force -Path $profileDir | Out-Null }
if (!(Test-Path $profilePath)) { New-Item -ItemType File -Force -Path $profilePath | Out-Null }

# Compute absolute path to scripts/cagent.ps1 relative to this script location
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot  = Resolve-Path (Join-Path $scriptDir "..")
$cagentPath = Resolve-Path (Join-Path $repoRoot "scripts/cagent.ps1")

$snippet = @"
# Added by cagent installer on $(Get-Date -Format o)
Set-Alias -Name cagent -Value "$cagentPath"
# Ensure execution policy allows local scripts (adjust as needed)
if ((Get-ExecutionPolicy -Scope CurrentUser) -eq 'Restricted') {
  Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force
}
"@

# Idempotently add or replace existing alias lines
$content = Get-Content $profilePath -Raw
$pattern = 'Set-Alias\s+-Name\s+cagent\s+-Value[^\r\n]*'
if ($content -match $pattern) {
  $replacement = "Set-Alias -Name cagent -Value `"$cagentPath`""
  $content = [regex]::Replace($content, $pattern, $replacement)
} else {
  $content += "`n$snippet`n"
}
Set-Content -Path $profilePath -Value $content -Encoding UTF8

Write-Host "[OK] PowerShell profile updated: $profilePath"
Write-Host "[OK] 'cagent' alias now points to: $cagentPath"
Write-Host "Open a new PowerShell window and run: cagent run seo"
