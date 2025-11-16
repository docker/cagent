param(
  [string]$EnvFile = ".env"
)

if (!(Test-Path data)) { if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null } }

# Prefer host env var; if missing, parse from the env file
$groq = $Env:GROQ_API_KEY
if (-not $groq -and (Test-Path $EnvFile)) {
  $line = Get-Content $EnvFile | Where-Object { $_ -match '^GROQ_API_KEY\s*=' } | Select-Object -First 1
  if ($line) {
    $groq = ($line -split '=',2)[1].Trim().Trim('"')
  }
}
if (-not $groq) {
  Write-Error "GROQ_API_KEY not found in host env or $EnvFile. Please set it or add to $EnvFile."
  exit 1
}

docker run --rm -it `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -e OPENAI_API_KEY=$groq `
  -v $pwdPath:/work `
  -w /work `
  ascathleticsinc/cagent:latest run examples/groq_general.yaml --debug
