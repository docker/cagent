param(
  [string]$EnvFile = ".env"
)

$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path data | Out-Null

# Resolve GROQ key from host env or .env
$groq = $Env:GROQ_API_KEY
if (-not $groq -and (Test-Path $EnvFile)) {
  $line = Get-Content $EnvFile | Where-Object { $_ -match '^GROQ_API_KEY\s*=' } | Select-Object -First 1
  if ($line) { $groq = ($line -split '=',2)[1].Trim().Trim('"') }
}
if (-not $groq) { throw "GROQ_API_KEY not found in host env or $EnvFile" }

$pwdPath = ${PWD}.Path

docker run --rm -it `
  --env-file "$EnvFile" `
  -e OPENAI_API_KEY=$groq `
  -v $pwdPath:/work `
  -w /work `
  ascathleticsinc/cagent:latest run examples/interactive_runner.yaml --debug
