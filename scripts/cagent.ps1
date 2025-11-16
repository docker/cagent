param(
  [Parameter(ValueFromRemainingArguments=$true)]
  [string[]]$Args
)
# PowerShell Docker shim so you can run: ./scripts/cagent.ps1 run seo
# To make it a one-liner "cagent run seo" in any shell, see install_ps_profile_snippet.ps1

$ErrorActionPreference = 'Stop'

# Resolve repo root and ensure we run at repo root for relative paths
# Resolve repo root based on script location so this works from any directory
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = Resolve-Path (Join-Path $scriptDir "..")
$envFile = Join-Path $repo ".env"
if (!(Test-Path $envFile)) {
  Write-Warning "No .env found at $envFile. Continuing, but OPENAI_API_KEY must be provided via environment."
}

# Choose provider key: prefer GROQ, fall back to OPENROUTER, else use OPENAI_API_KEY if already set
$openaiKey = $Env:OPENAI_API_KEY
if (-not $openaiKey) {
  if ($Env:GROQ_API_KEY) { $openaiKey = $Env:GROQ_API_KEY }
  elseif ($Env:OPENROUTER_API_KEY) { $openaiKey = $Env:OPENROUTER_API_KEY }
}
if (-not $openaiKey -and (Test-Path $envFile)) {
  $lines = Get-Content $envFile
  $groqLine = $lines | Where-Object { $_ -match '^GROQ_API_KEY\s*=' } | Select-Object -First 1
  $orLine   = $lines | Where-Object { $_ -match '^OPENROUTER_API_KEY\s*=' } | Select-Object -First 1
  if ($groqLine) { $openaiKey = ($groqLine -split '=',2)[1].Trim().Trim('"') }
  elseif ($orLine) { $openaiKey = ($orLine -split '=',2)[1].Trim().Trim('"') }
}
if (-not $openaiKey) {
  throw "No API key found. Set GROQ_API_KEY or OPENROUTER_API_KEY in .env or OPENAI_API_KEY in env."
}

# Mount host aliases and config into container so "cagent run <alias>" works and persists
$hostCagentDir = Join-Path $Env:USERPROFILE ".cagent"
if (!(Test-Path $hostCagentDir)) { New-Item -ItemType Directory -Force -Path $hostCagentDir | Out-Null }
$hostConfigDir = Join-Path $Env:USERPROFILE ".config\cagent"
if (!(Test-Path $hostConfigDir)) { New-Item -ItemType Directory -Force -Path $hostConfigDir | Out-Null }

$pwdPath = ${PWD}.Path

# If 'run' was requested, pre-warm the terminal agent (acli rovodev) and send '/yolo' on a timer
function Invoke-TerminalAgent {
  param([int]$FirstDelay=15,[int]$SecondDelay=10)
  try {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = "acli"
    $psi.Arguments = "rovodev"
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $false
    $psi.RedirectStandardError = $false
    $proc = New-Object System.Diagnostics.Process
    $proc.StartInfo = $psi
    $null = $proc.Start()
    Start-Sleep -Seconds $FirstDelay
    $proc.StandardInput.WriteLine('/yolo')
    $proc.StandardInput.Flush()
    Start-Sleep -Seconds $SecondDelay
    return $proc
  } catch {
    Write-Warning "Failed to start or communicate with 'acli rovodev': $($_.Exception.Message)"
    return $null
  }
}

# Run cagent in Docker, forwarding all arguments
# Examples:
#   ./scripts/cagent.ps1 run seo
#   ./scripts/cagent.ps1 run examples/content_creator.yaml --debug

# Pre-hook: if first arg is 'run', invoke the terminal agent automation
$terminalProc = $null
if ($Args.Count -ge 1 -and $Args[0] -eq 'run') {
  Write-Host "[acli] Starting 'acli rovodev' and waiting 15s, then sending /yolo and waiting 10s..."
  $terminalProc = Invoke-TerminalAgent -FirstDelay 1 -SecondDelay 1
  if ($terminalProc -eq $null) { Write-Warning "[acli] Could not start or communicate with terminal agent. Continuing without it." } else { Write-Host "[acli] Trigger sent. Proceeding to run cagent..." }

  # Auto-alias seeding: if 'cagent run <alias>' is used and <alias>.yaml exists under examples,
  # ensure the alias is registered so invocation works from any directory.
  if ($Args.Count -ge 2) {
    $maybeAlias = $Args[1]
    $isPath = ($maybeAlias -like "*/*.yaml" -or $maybeAlias -like "*.yaml")
    if (-not $isPath) {
      $exampleFile = Join-Path $repo (Join-Path "examples" ("$maybeAlias.yaml"))
      if (Test-Path $exampleFile) {
        Write-Host "[alias] Seeding alias '$maybeAlias' -> /work/examples/$maybeAlias.yaml"
        $aliasArgs = @(
          "run","--rm",
          "--env-file", $envFile,
          "-e","OPENAI_API_KEY=$openaiKey",
          "-v","${pwdPath}:/work",
          "-v","${hostCagentDir}:/home/cagent/.cagent",
          "-v","${hostConfigDir}:/home/cagent/.config/cagent",
          "-w","/work",
          "ascathleticsinc/cagent:latest",
          "alias","add", $maybeAlias, ("/work/examples/$maybeAlias.yaml")
        )
        $aliasExit = (Start-Process -FilePath docker -ArgumentList $aliasArgs -NoNewWindow -PassThru -Wait).ExitCode
        if ($aliasExit -ne 0) { Write-Warning "[alias] Failed to seed alias '$maybeAlias' (exit $aliasExit). Continuing..." }
      }
    }
  }
}

$dockerArgs = @(
  "run","--rm","-it",
  "--env-file", $envFile,
  "-e","OPENAI_API_KEY=$openaiKey",
  "-v","${pwdPath}:/work",
  "-v","${hostCagentDir}:/home/cagent/.cagent",
  "-v","${hostConfigDir}:/home/cagent/.config/cagent",
  "-w","/work",
  "ascathleticsinc/cagent:latest"
)
$dockerArgs += $Args

# If no args provided, default to the interactive runner
if ($Args.Count -eq 0) {
  $dockerArgs += @("run","examples/interactive_runner.yaml","--debug")
}

Write-Host "docker $($dockerArgs -join ' ')"
$exit = (Start-Process -FilePath docker -ArgumentList $dockerArgs -NoNewWindow -PassThru -Wait).ExitCode

# Optional: after cagent run finishes, wait for acli to end or stop it
if ($terminalProc -ne $null) {
  try { if (-not $terminalProc.HasExited) { $terminalProc.CloseMainWindow() | Out-Null } } catch {}
}

exit $exit
