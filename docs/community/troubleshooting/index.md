---
title: "Troubleshooting"
description: "Common issues and how to resolve them when working with docker-agent."
permalink: /community/troubleshooting/
---

# Troubleshooting

_Common issues and how to resolve them when working with docker-agent._

## Common Errors

### Context Window Exceeded

Error message: `context_length_exceeded` or similar.

- Use `/compact` in the TUI to summarize and reduce conversation history
- Set `num_history_items` in agent config to limit messages sent to the model
- Switch to a model with larger context (e.g., Claude 200K, Gemini 2M)
- Break large tasks into smaller conversations

### Max Iterations Reached

The agent hit its `max_iterations` limit without completing the task.

- Increase `max_iterations` in agent config (default is unlimited, but many agents set 20-50)
- Check if the agent is stuck in a loop (enable `--debug` to see tool calls)
- Break complex tasks into smaller steps

### Model Fallback Triggered

When the primary model fails, docker-agent automatically switches to fallback models. Look for log messages like `"Switching to fallback model"`.

- **429 errors:** Rate limited — the cooldown period keeps using the fallback
- **5xx errors:** Server issues — retries with exponential backoff first, then falls back
- **4xx errors:** Client errors — skips directly to next model

Configure fallback behavior in your agent config:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-0
    fallback:
      models: [openai/gpt-4o, openai/gpt-4o-mini]
      retries: 2 # retries per model for 5xx errors
      cooldown: 1m # how long to stick with fallback after 429
```

## Debug Mode

The first step for any issue is enabling debug logging. This provides detailed information about what docker-agent is doing internally.

```bash
# Enable debug logging (writes to ~/.cagent/cagent.debug.log)
$ docker agent run config.yaml --debug

# Write debug logs to a custom file
$ docker agent run config.yaml --debug --log-file ./debug.log

# Enable OpenTelemetry tracing for deeper analysis
$ docker agent run config.yaml --otel
```

<div class="callout callout-tip">
<div class="callout-title">💡 Tip
</div>
  <p>Always enable <code>--debug</code> when reporting issues. The log file contains detailed traces of API calls, tool executions, and agent interactions.</p>

</div>

## Agent Not Responding

### API keys not set

Each model provider requires its own API key as an environment variable:

| Provider      | Environment Variable                                |
| ------------- | --------------------------------------------------- |
| OpenAI        | `OPENAI_API_KEY`                                    |
| Anthropic     | `ANTHROPIC_API_KEY`                                 |
| Google Gemini | `GOOGLE_API_KEY`                                    |
| Mistral       | `MISTRAL_API_KEY`                                   |
| xAI           | `XAI_API_KEY`                                       |
| AWS Bedrock   | `AWS_BEARER_TOKEN_BEDROCK` or AWS credentials chain |

```bash
# Verify your keys are set
$ env | grep API_KEY
```

### Incorrect model name

Model names must match the provider's naming exactly. Common mistakes:

- Using `gpt-4` instead of `gpt-4o`
- Using a deprecated model name
- Model references are case-sensitive: `openai/gpt-4o` ≠ `openai/GPT-4o`

### Network connectivity

If the agent hangs or times out, check that you can reach the provider's API endpoint. Firewalls, VPNs, or proxy settings may block requests.

## Tool Execution Failures

### MCP tools not found or failing

- Ensure the MCP tool command is installed and on your `PATH`
- Check file permissions — tools need to be executable
- Test MCP tools independently before integrating with docker-agent
- For Docker-based MCP tools (`ref: docker:*`), ensure Docker Desktop is running

### Filesystem / shell tool errors

- Verify the agent has the correct toolset configured (`type: filesystem`, `type: shell`)
- Check that the working directory exists and is accessible
- On macOS, ensure terminal has the necessary permissions (e.g., Full Disk Access)

### Tool lifecycle issues

MCP tools using stdio transport must complete the initialization handshake before becoming available. If tools fail silently:

1. Enable `--debug` and look for MCP protocol messages in the log
2. Check that the MCP server process starts and responds to `initialize`
3. Verify environment variables required by the tool are set (check `env` and `env_file` in the toolset config)

## Configuration Errors

### YAML syntax issues

docker-agent validates config at startup and reports errors with line numbers. Common problems:

- Incorrect indentation (YAML is whitespace-sensitive)
- Missing quotes around values containing special characters (`:`, `#`, `{`, `}`)
- Using tabs instead of spaces

### Missing references

- All agents in `sub_agents` must be defined in the `agents` section
- Named model references must exist in the `models` section (or use inline format like `openai/gpt-4o`)
- RAG source names referenced by agents must be defined in the `rag` section

### Toolset validation

- The `path` field is only valid for `memory` toolsets
- MCP toolsets need either `command` (stdio), `remote` (SSE/HTTP), or `ref` (Docker)
- Provider names must be one of: `openai`, `anthropic`, `google`, `amazon-bedrock`, `dmr`, etc.

<div class="callout callout-info">
<div class="callout-title">ℹ️ Schema Validation
</div>
  <p>Use the <a href="https://github.com/docker/cagent/blob/main/agent-schema.json">JSON schema</a> in your editor for real-time config validation and autocompletion.</p>

</div>

## Session &amp; Connectivity Issues

### Port conflicts

When docker-agent as an API server or MCP server, ensure the port is not already in use:

```bash
# Check if port 8080 is in use
$ lsof -i :8080

# Use a different port
$ docker agent api config.yaml --listen :9090
```

### MCP endpoint accessibility

For remote MCP servers, verify the endpoint is reachable:

```bash
# Test SSE endpoint
$ curl -v https://mcp-server.example.com/sse
```

### Multi-tenant isolation

In API server mode, each client gets isolated sessions. If sessions are mixing up:

- Verify client IDs are unique per connection
- Check session timeouts and cleanup in debug logs

## Performance Issues

### High memory usage

- Large context windows (64K+ tokens) consume significant memory — consider reducing `max_tokens`
- Use `num_history_items` in agent config to limit conversation history
- For DMR (local models), tune `runtime_flags` for your hardware (e.g., `--ngl` for GPU layers)

### Slow responses

- Check if MCP tools are adding latency (visible in debug logs)
- Use the `/cost` command in TUI to see token usage and identify expensive interactions
- For DMR, consider enabling [speculative decoding](/providers/dmr/) for faster inference

### Tool resource leaks

Monitor for tools that don't clean up properly — check debug logs for MCP server start/stop lifecycle events. Orphaned tool processes can consume system resources.

## Agent Store Issues

### Pull / push failures

```bash
# Test registry connectivity
$ docker pull docker.io/username/agent:latest

# Verify pulled agent content
$ docker agent pull docker.io/username/agent:latest
```

### Agent content issues

- Ensure the pushed YAML is valid — run `docker agent run` locally before pushing
- Check that referenced resources (MCP tools, files) are available on the target machine
- For auto-refresh (`--pull-interval`), verify the registry is accessible from the server

## Log Analysis

When reviewing debug logs, search for these key patterns:

| Log Pattern                 | What It Indicates                                                          |
| --------------------------- | -------------------------------------------------------------------------- |
| `"Starting runtime stream"` | Agent execution beginning                                                  |
| `"Tool call"`               | A tool is being executed                                                   |
| `"Tool call result"`        | Tool execution completed                                                   |
| `"Stream stopped"`          | Agent finished processing                                                  |
| `HTTP 429`                  | Rate limiting — consider adding a [fallback model](/configuration/agents/) |
| `context canceled`          | Operation was interrupted (timeout or user cancel)                         |
| `[RAG Manager]`             | RAG retrieval operations                                                   |
| `[Reranker]`                | Reranking operations                                                       |

<div class="callout callout-warning">
<div class="callout-title">⚠️ Still stuck?
</div>
  <p>If these steps don't resolve your issue, file a bug on the <a href="https://github.com/docker/cagent/issues">GitHub issue tracker</a> with your debug log attached, or ask on <a href="https://dockercommunity.slack.com/archives/C09DASHHRU4">Slack</a>.</p>

</div>
