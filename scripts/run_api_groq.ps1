param(
  [string]$EnvFile = ".env",
  [string]$Listen = ":8080",
  [string]$AgentPath = "/work/examples/groq_general.yaml"
)

if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null }

# Resolve GROQ key from host env or .env
$groq = $Env:GROQ_API_KEY
if (-not $groq -and (Test-Path $EnvFile)) {
  $line = Get-Content $EnvFile | Where-Object { $_ -match '^GROQ_API_KEY\s*=' } | Select-Object -First 1
  if ($line) { $groq = ($line -split '=',2)[1].Trim().Trim('"') }
}
if (-not $groq) {
  Write-Error "GROQ_API_KEY not found in host env or $EnvFile. Please set it or add to $EnvFile."
  exit 1
}

# Run API server exposing the port
# NOTE: api command REQUIRES an agent file or a directory argument
# Using $AgentPath (default: /work/examples/groq_general.yaml)
docker run --rm -it `
  --env-file "$EnvFile" `
  -e OPENAI_API_KEY=$groq `
  -p $($Listen.TrimStart(':')):$($Listen.TrimStart(':')) `
  -v ${PWD}.Path:/work `
  -w /work `
  ascathleticsinc/cagent:latest api $AgentPath --listen $Listen
