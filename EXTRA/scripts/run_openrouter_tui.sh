#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_openrouter_tui.sh [/path/to/.env]
ENV_FILE=${1:-".env"}

mkdir -p data

# Prefer OPENROUTER_API_KEY; fall back to OPENAI_API_KEY if already exported
exec docker run --rm -it \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -e OPENAI_API_KEY=${OPENROUTER_API_KEY:-${OPENAI_API_KEY:-}} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent run examples/openrouter_general.yaml --debug
