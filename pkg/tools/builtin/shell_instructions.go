package builtin

// nativeInstructions contains the usage guide for native shell mode.
const nativeInstructions = `# Shell Tool Usage Guide

Execute shell commands in the user's environment with full control over working directories and command parameters.

## Core Concepts

- Commands run in the user's default shell (Unix: ${SHELL} or /bin/sh; Windows: pwsh/powershell or cmd.exe)
- Each call creates a fresh shell session — no state persists between executions
- Default working directory: agent's working directory. Override with "cwd" parameter (absolute or relative paths)
- Default timeout: 30s. Use "timeout" parameter for longer operations (e.g., builds, tests)

## Best Practices

- Use "cwd" instead of cd within commands
- Quote arguments with spaces or special characters
- Use pipes, redirections, and heredocs to combine operations
- Prefer inline heredocs over writing shell scripts to disk
- For git commits, add trailer: git commit -m "message" -m "" -m "Assisted-By: cagent"

## Examples

{ "cmd": "go test ./...", "cwd": ".", "timeout": 180 }
{ "cmd": "grep -r 'TODO' src/", "cwd": "." }
{ "cmd": "cat << 'EOF' | ${SHELL}\necho Hello\nEOF" }

## Error Handling

Non-zero exit codes return error info with output. Timed-out commands are terminated automatically.

# Background Jobs

Use background jobs for long-running processes (servers, watchers) that should run while other tasks are performed.

- **run_background_job**: Start a command, returns job ID. Example: { "cmd": "npm run dev", "cwd": "frontend" }
- **list_background_jobs**: Show all jobs with status and runtime
- **view_background_job**: Get output and status of a job by job_id
- **stop_background_job**: Terminate a job and all its child processes by job_id

**Notes**: Output capped at 10MB per job. Jobs inherit env vars at start time. All jobs auto-terminate when the agent stops.`
