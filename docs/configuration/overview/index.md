---
title: "Configuration Overview"
description: "docker-agent uses YAML configuration files to define agents, models, tools, and their relationships."
permalink: /configuration/overview/
---

# Configuration Overview

_docker-agent uses YAML configuration files to define agents, models, tools, and their relationships._

## File Structure

A docker-agent YAML config has these main sections:

```bash
# 1. Version — configuration schema version (optional but recommended)
version: 6

# 2. Metadata — optional agent metadata for distribution
metadata:
  author: my-org
  description: My helpful agent
  version: "1.0.0"

# 3. Models — define AI models with their parameters
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000

# 4. Agents — define AI agents with their behavior
agents:
  root:
    model: claude
    description: A helpful assistant
    instruction: You are helpful.
    toolsets:
      - type: think

# 5. RAG — define retrieval-augmented generation sources (optional)
rag:
  docs:
    docs: ["./docs"]
    strategies:
      - type: chunked-embeddings
        model: openai/text-embedding-3-small

# 6. Providers — optional custom provider definitions
providers:
  my_provider:
    api_type: openai_chatcompletions
    base_url: https://api.example.com/v1
    token_key: MY_API_KEY

# 7. Permissions — global tool permission rules (optional)
permissions:
  allow: ["read_*"]
  deny: ["shell:cmd=sudo*"]
```

## Minimal Config

The simplest possible configuration — a single agent with an inline model:

```yaml
agents:
  root:
    model: openai/gpt-4o
    description: A helpful assistant
    instruction: You are a helpful assistant.
```

## Inline vs Named Models

Models can be referenced inline or defined in the `models` section:

<div class="cards">
  <div class="card" style="cursor:default;">
    <h3>Inline</h3>
    <p>Quick and simple. Use <code>provider/model</code> syntax directly.</p>
    <pre style="margin-top:12px"><code class="language-yaml">model: openai/gpt-4o</code></pre>
  </div>
  <div class="card" style="cursor:default;">
    <h3>Named</h3>
    <p>Full control over parameters. Reusable across agents.</p>
    <pre style="margin-top:12px"><code class="language-yaml">model: my_claude</code></pre>
  </div>
</div>

## Config Sections

<div class="cards">
  <a class="card" href="{{ '/configuration/agents/' | relative_url }}">
    <div class="card-icon">🤖</div>
    <h3>Agent Config</h3>
    <p>All agent properties: model, instruction, tools, sub-agents, hooks, and more.</p>
  </a>
  <a class="card" href="{{ '/configuration/models/' | relative_url }}">
    <div class="card-icon">🧠</div>
    <h3>Model Config</h3>
    <p>Provider setup, parameters, thinking budget, and provider-specific options.</p>
  </a>
  <a class="card" href="{{ '/configuration/tools/' | relative_url }}">
    <div class="card-icon">🔧</div>
    <h3>Tool Config</h3>
    <p>Built-in tools, MCP tools, Docker MCP, LSP, API tools, and tool filtering.</p>
  </a>
</div>

## Advanced Configuration

<div class="cards">
  <a class="card" href="{{ '/configuration/hooks/' | relative_url }}">
    <div class="card-icon">⚡</div>
    <h3>Hooks</h3>
    <p>Run shell commands at lifecycle events like tool calls and session start/end.</p>
  </a>
  <a class="card" href="{{ '/configuration/permissions/' | relative_url }}">
    <div class="card-icon">🔐</div>
    <h3>Permissions</h3>
    <p>Control which tools auto-approve, require confirmation, or are blocked.</p>
  </a>
  <a class="card" href="{{ '/configuration/sandbox/' | relative_url }}">
    <div class="card-icon">📦</div>
    <h3>Sandbox Mode</h3>
    <p>Run agents in an isolated Docker container for security.</p>
  </a>
  <a class="card" href="{{ '/configuration/structured-output/' | relative_url }}">
    <div class="card-icon">📋</div>
    <h3>Structured Output</h3>
    <p>Constrain agent responses to match a specific JSON schema.</p>
  </a>
</div>

## Environment Variables

API keys and secrets are read from environment variables — never stored in config files:

| Variable            | Provider      |
| ------------------- | ------------- |
| `OPENAI_API_KEY`    | OpenAI        |
| `ANTHROPIC_API_KEY` | Anthropic     |
| `GOOGLE_API_KEY`    | Google Gemini |
| `MISTRAL_API_KEY`   | Mistral       |
| `XAI_API_KEY`       | xAI           |
| `NEBIUS_API_KEY`    | Nebius        |

**Tool Auto-Installation:**

| Variable              | Description                                                     |
| --------------------- | --------------------------------------------------------------- |
| `DOCKER_AGENT_AUTO_INSTALL` | Set to `false` to disable automatic tool installation           |
| `DOCKER_AGENT_TOOLS_DIR`    | Override the base directory for installed tools (default: `~/.cagent/tools/`) |

<div class="callout callout-warning">
<div class="callout-title">⚠️ Important
</div>
  <p>Model references are case-sensitive: <code>openai/gpt-4o</code> is not the same as <code>openai/GPT-4o</code>.</p>

</div>

## Validation

docker-agent validates your configuration at startup:

- All `sub_agents` must reference agents defined in the config
- Named model references must exist in the `models` section
- Provider names must be valid (`openai`, `anthropic`, `google`, `dmr`, etc.)
- Required environment variables (API keys) must be set
- Tool-specific fields are validated (e.g., `path` is only valid for `memory`)

## JSON Schema

For editor autocompletion and validation, use the [Docker Agent JSON Schema](https://github.com/docker/cagent/blob/main/agent-schema.json). Add this to the top of your YAML file:

```bash
# yaml-language-server: $schema=https://raw.githubusercontent.com/docker/docker-agent/main/agent-schema.json
```

## Config Versioning

docker-agent configs are versioned. The current version is `5`. Add the version at the top of your config:

```yaml
version: 5

agents:
  root:
    model: openai/gpt-4o
    # ...
```

When you load an older config, docker-agent automatically migrates it to the latest schema. It's recommended to include the version to ensure consistent behavior.

## Metadata Section

Optional metadata for agent distribution via OCI registries:

```yaml
metadata:
  author: my-org
  license: Apache-2.0
  description: A helpful coding assistant
  readme: | # Displayed in registries
    This agent helps with coding tasks.
  version: "1.0.0"
```

| Field         | Description                                |
| ------------- | ------------------------------------------ |
| `author`      | Author or organization name                |
| `license`     | License identifier (e.g., Apache-2.0, MIT) |
| `description` | Short description for the agent            |
| `readme`      | Longer markdown description                |
| `version`     | Semantic version string                    |

See [Agent Distribution](/concepts/distribution/) for publishing agents to registries.

## Custom Providers Section

Define reusable provider configurations for custom or self-hosted endpoints:

```yaml
providers:
  azure:
    api_type: openai_chatcompletions
    base_url: https://my-resource.openai.azure.com/openai/deployments/gpt-4o
    token_key: AZURE_OPENAI_API_KEY

  internal_llm:
    api_type: openai_chatcompletions
    base_url: https://llm.internal.company.com/v1
    token_key: INTERNAL_API_KEY

models:
  azure_gpt:
    provider: azure # References the custom provider
    model: gpt-4o

agents:
  root:
    model: azure_gpt
```

| Field       | Description                                                          |
| ----------- | -------------------------------------------------------------------- |
| `api_type`  | API schema: `openai_chatcompletions` (default) or `openai_responses` |
| `base_url`  | Base URL for the API endpoint                                        |
| `token_key` | Environment variable name for the API token                          |

See [Custom Providers](/providers/custom/) for more details.
