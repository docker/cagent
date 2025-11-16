param(
  [string]$EnvFile = ".env",
  [string]$Listen = ":8080",
  [string]$AgentPath = "/work/examples/openrouter_general.yaml"
)

if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null }

# Resolve OpenRouter key from host env or .env
$orKey = $Env:OPENROUTER_API_KEY
if (-not $orKey -and (Test-Path $EnvFile)) {
  $line = Get-Content $EnvFile | Where-Object { $_ -match '^OPENROUTER_API_KEY\s*=' } | Select-Object -First 1
  if ($line) { $orKey = ($line -split '=',2)[1].Trim().Trim('"') }
}
if (-not $orKey) {
  Write-Error "OPENROUTER_API_KEY not found in host env or $EnvFile. Please set it or add to $EnvFile."
  exit 1
}

# Run API server exposing the port
# api requires agent file or directory argument

docker run --rm -it `
  --env-file "$EnvFile" `
  -e OPENAI_API_KEY=$orKey `
  -p $($Listen.TrimStart(':')):$($Listen.TrimStart(':')) `
  -v ${PWD}.Path:/work `
  -w /work `
  ascathleticsinc/cagent:latest api $AgentPath --listen $Listen
