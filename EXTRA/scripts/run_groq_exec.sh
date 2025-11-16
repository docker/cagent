#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_groq_exec.sh "Your prompt" [/path/to/.env]
PROMPT=${1:-"Say hello from Groq"}
ENV_FILE=${2:-".env"}

mkdir -p data

exec docker run --rm \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -e OPENAI_API_KEY=${GROQ_API_KEY:-$OPENAI_API_KEY} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent exec examples/groq_general.yaml "$PROMPT"
