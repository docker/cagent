# QUICKSTART

This guide shows how to run cagent quickly on Ubuntu (bash) and Windows (PowerShell), using the official Docker image and the example configs added in this repo.

- Image: `docker/cagent`
- Example configs:
  - `examples/groq_general.yaml` (Groq via OpenAI-compatible API)
  - `examples/kimi_general.yaml` (Moonshot AI Kimi via OpenAI-compatible API)
- Data: memory databases are stored under `./data` (host-mounted into `/work/data` in the container)

If you don't have Docker installed yet:
- Ubuntu: https://docs.docker.com/engine/install/ubuntu/
- Windows: https://docs.docker.com/desktop/install/windows-install/

---

## 1) Pull the image

```bash
# Ubuntu / bash
docker pull docker/cagent:latest
```

```powershell
# Windows PowerShell
docker pull docker/cagent:latest
```

---

## 2) Environment variables

You need a provider API key based on the model you use. Put keys in an `.env` file (recommended) or pass them inline.

Examples:

```bash
# Groq
GROQ_API_KEY=sk_groq_xxx
TELEMETRY_ENABLED=false
```

```bash
# Moonshot (Kimi)
MOONSHOT_API_KEY=sk_moonshot_xxx
TELEMETRY_ENABLED=false
```

Windows example `.env` path: `c:\cagent\cagent\.env`

---

## 3) Ubuntu: Start scripts (bash)

Create a `scripts` folder and add these helper scripts. Make sure they are executable: `chmod +x scripts/*.sh`.

### scripts/run_groq_tui.sh
```bash
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
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent run examples/groq_general.yaml --debug
```

### scripts/run_groq_exec.sh
```bash
#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/run_groq_exec.sh "Your prompt" [/path/to/.env]
PROMPT=${1:-"Say hello from Groq"}
ENV_FILE=${2:-".env"}

mkdir -p data

exec docker run --rm \
  --env-file "$ENV_FILE" \
  -e TELEMETRY_ENABLED=${TELEMETRY_ENABLED:-false} \
  -v "$(pwd)":/work \
  -w /work \
  docker/cagent exec examples/groq_general.yaml "$PROMPT"
```

### scripts/run_kimi_tui.sh
```bash
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
```

### scripts/run_kimi_exec.sh
```bash
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
```

---

## 4) Windows: Start scripts (PowerShell)

Create a `scripts` folder and add these `.ps1` scripts. You can run them in PowerShell. If needed, adjust Execution Policy (e.g., `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`).

### scripts/run_groq_tui.ps1
```powershell
param(
  [string]$EnvFile = ".env"
)

New-Item -ItemType Directory -Force -Path data | Out-Null

docker run --rm -it `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -v ${PWD}:/work `
  -w /work `
  docker/cagent run examples/groq_general.yaml --debug
```

### scripts/run_groq_exec.ps1
```powershell
param(
  [string]$Prompt = "Say hello from Groq",
  [string]$EnvFile = ".env"
)

New-Item -ItemType Directory -Force -Path data | Out-Null

docker run --rm `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -v ${PWD}:/work `
  -w /work `
  docker/cagent exec examples/groq_general.yaml "$Prompt"
```

### scripts/run_kimi_tui.ps1
```powershell
param(
  [string]$EnvFile = ".env"
)

New-Item -ItemType Directory -Force -Path data | Out-Null

docker run --rm -it `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -v ${PWD}:/work `
  -w /work `
  docker/cagent run examples/kimi_general.yaml --debug
```

### scripts/run_kimi_exec.ps1
```powershell
param(
  [string]$Prompt = "Say hello from Kimi",
  [string]$EnvFile = ".env"
)

New-Item -ItemType Directory -Force -Path data | Out-Null

docker run --rm `
  --env-file "$EnvFile" `
  -e TELEMETRY_ENABLED=$Env:TELEMETRY_ENABLED `
  -v ${PWD}:/work `
  -w /work `
  docker/cagent exec examples/kimi_general.yaml "$Prompt"
```

---

## 5) Testing common functions

Try these prompts to exercise tools and memory:

- Filesystem (write):
  - "Create a file named hello.txt with the content: Hello from cagent."
- Filesystem (read):
  - "Read the contents of hello.txt"
- Shell:
  - "Run a shell command: echo shell-ok"
- Memory (persisted in `./data/*.db`):
  - "Remember that the project codename is Aurora"
  - Later: "What is the project codename?"

---

## 6) Notes

- The example agents use provider=\"openai\" with custom `base_url` and `token_key` to support OpenAI-compatible vendors:
  - Groq: `base_url=https://api.groq.com/openai/v1`, `token_key=GROQ_API_KEY`
  - Moonshot/Kimi: `base_url=https://api.moonshot.cn/v1`, `token_key=MOONSHOT_API_KEY`
- Set only the keys you actually use in your `.env`.
- Set `TELEMETRY_ENABLED=false` if you want to disable anonymous usage telemetry.
- For more configs, see the `examples/` folder.
