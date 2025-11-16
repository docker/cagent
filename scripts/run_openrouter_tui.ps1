param(
  [string]$EnvFile = ".env"
)

if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null }

# Prefer host env var; if missing, parse from the env file
$orKey = $Env:OPENROUTER_API_KEY
if (-not $orKey -and (Test-Path $EnvFile)) {
  $line = Get-Content $EnvFile | Where-Object { $_ -match '^OPENROUTER_API_KEY\s*=' } | Select-Object -First 1
  if ($line) {
    $orKey = ($line -split '=',2)[1].Trim().Trim('"')
  }
}
if (-not $orKey) {
  Write-Error "OPENROUTER_API_KEY not found in host env or $EnvFile. Please set it or add to $EnvFile."
  exit 1
}

docker run --rm -it `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -e OPENAI_API_KEY=$orKey `
  -v ${PWD}:/work `
  -w /work `
  ascathleticsinc/cagent:latest run examples/openrouter_general.yaml --debug
