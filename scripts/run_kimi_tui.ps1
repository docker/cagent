param(
  [string]$EnvFile = ".env"
)

if (!(Test-Path data)) { New-Item -ItemType Directory -Force -Path data | Out-Null }

docker run --rm -it `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -e OPENAI_API_KEY=$Env:MOONSHOT_API_KEY `
  -v ${PWD}:/work `
  -w /work `
  ascathleticsinc/cagent:latest run examples/kimi_general.yaml --debug
