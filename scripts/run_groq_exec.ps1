param(
  [string]$Prompt = "Say hello from Groq",
  [string]$EnvFile = ".env"
)

New-Item -ItemType Directory -Force -Path data | Out-Null

docker run --rm `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -e OPENAI_API_KEY=$Env:GROQ_API_KEY `
  -v ${PWD}:/work `
  -w /work `
  ascathleticsinc/cagent:latest exec examples/groq_general.yaml "$Prompt"
