#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_kimi_exec.sh "Your prompt" [/path/to/.env]
PROMPT=${1:-"Say hello from Kimi"}
ENV_FILE=${2:-".env"}

mkdir -p data

exec docker run --rm \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent exec examples/kimi_general.yaml "$PROMPT"
