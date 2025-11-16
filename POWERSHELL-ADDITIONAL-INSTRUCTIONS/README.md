# PowerShell Additional Instructions

This guide is tailored for Windows PowerShell users to run and manage cagent with Groq (primary) and OpenRouter (secondary) providers using Docker.

## Prerequisites
- Windows 10/11 with Docker Desktop installed
- PowerShell 5.1+ or PowerShell 7+
- .env file with your keys at the repo root (e.g., `C:\cagent\cagent\.env`)

Example `.env` entries:
```
GROQ_API_KEY=sk_groq_...
OPENROUTER_API_KEY=sk-or-v1-...
TELEMETRY_ENABLED=false
```

## Quick commands

### 1) Run interactive TUI with Groq
```powershell
scripts/run_groq_tui.ps1 -EnvFile ".env"
```

### 2) Run interactive TUI with OpenRouter
```powershell
scripts/run_openrouter_tui.ps1 -EnvFile ".env"
```

### 3) One-shot (no TUI)
```powershell
scripts/run_groq_exec.ps1 -Prompt "Say hello from Groq" -EnvFile ".env"
```

## Exposing an API port (8080)
TUI mode doesn’t expose a port; for HTTP access, run API mode.

### One-off docker run
```powershell
# Starts cagent API server on localhost:8080
# Maps GROQ_API_KEY to OPENAI_API_KEY inside the container

docker run --rm -it `
  --env-file ".env" `
  -e OPENAI_API_KEY=$Env:GROQ_API_KEY `
  -p 8080:8080 `
  -v ${PWD}:/work `
  -w /work `
  docker/cagent:latest api --listen :8080
```

Verify:
```powershell
curl http://localhost:8080/api/agents
curl http://localhost:8080/api/sessions
```

### Docker Compose service (optional)
```yaml
services:
  cagent:
    image: docker/cagent:latest
    env_file: [.env]
    environment:
      TELEMETRY_ENABLED: "false"
      OPENAI_API_KEY: ${GROQ_API_KEY}
    working_dir: /work
    volumes:
      - ./:/work
    command: ["api", "--listen", ":8080"]
    ports:
      - "8080:8080"
    tty: true
    stdin_open: true
```
Run:
```powershell
# From repo root
docker compose up
```

## Troubleshooting
- Missing OPENAI_API_KEY: Ensure your `.env` has GROQ_API_KEY or OPENROUTER_API_KEY. The scripts map these to OPENAI_API_KEY.
- Permission issues: Try PowerShell as Administrator and check Docker Desktop is running.
- Port in use: Change the host port mapping, e.g., `-p 8081:8080`.
- Verify environment:
```powershell
docker run --rm --env-file ".env" -e OPENAI_API_KEY=$Env:GROQ_API_KEY docker/cagent:latest env
```

## Useful PowerShell helpers
- List containers:
```powershell
docker ps
```
- Logs:
```powershell
docker logs <container_name> --tail 200
```
- Exec shell:
```powershell
docker exec -it <container_name> sh
```

## Notes
- The Groq model is set to `llama-3.3-70b-versatile` in `examples/groq*.yaml`.
- If you prefer OpenRouter, use `scripts/run_openrouter_tui.ps1 -EnvFile ".env"`.
- API mode only: the `api` command with `--listen :8080` exposes the HTTP interface.
