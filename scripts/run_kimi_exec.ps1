param(
  [string]$Prompt = "Say hello from Kimi",
  [string]$EnvFile = ".env"
)

if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null }

docker run --rm `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -e OPENAI_API_KEY=$Env:MOONSHOT_API_KEY `
  -v ${PWD}:/work `
  -w /work `
  ascathleticsinc/cagent:latest exec examples/kimi_general.yaml "$Prompt"
