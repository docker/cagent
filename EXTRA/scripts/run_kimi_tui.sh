#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_kimi_tui.sh [/path/to/.env]
ENV_FILE=${1:-".env"}

mkdir -p data

exec docker run --rm -it \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent run examples/kimi_general.yaml --debug
