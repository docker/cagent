#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_groq_tui.sh [/path/to/.env]
ENV_FILE=${1:-".env"}

# Ensure data dir exists
mkdir -p data

# Run interactive TUI with Groq-backed agent
exec docker run --rm -it \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -e OPENAI_API_KEY=${GROQ_API_KEY:-$OPENAI_API_KEY} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent run examples/groq_general.yaml --debug
