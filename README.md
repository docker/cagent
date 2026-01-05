# 🤖 cagent

A multi-agent runtime for orchestrating AI agents with specialized capabilities and tools.

![cagent in action](docs/demo.gif)

> ⚠️ **Note:** cagent is in active development. Breaking changes are expected.

## Quick Start

### Installation

```sh
# Using Homebrew
brew install cagent

# Or download from GitHub releases
# https://github.com/docker/cagent/releases
```

### Set API Keys

```bash
export OPENAI_API_KEY=your_key      # For OpenAI models
export ANTHROPIC_API_KEY=your_key   # For Anthropic models
export GOOGLE_API_KEY=your_key      # For Gemini models
export XAI_API_KEY=your_key         # For xAI models
export NEBIUS_API_KEY=your_key      # For Nebius models
export MISTRAL_API_KEY=your_key     # For Mistral models
```

### Create Your First Agent

Create `basic_agent.yaml`:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
```

Run it:

```bash
cagent run basic_agent.yaml
```

## Features

- **Multi-agent architecture** – Create specialized agents for different domains
- **MCP tool support** – Connect agents to external tools and services
- **Smart delegation** – Agents automatically route tasks to specialists
- **YAML configuration** – Simple declarative setup
- **RAG support** – Built-in retrieval-augmented generation
- **Multiple providers** – OpenAI, Anthropic, Gemini, xAI, Mistral, Nebius, and [Docker Model Runner](https://docs.docker.com/ai/model-runner/)

## Adding MCP Tools

Give agents access to external tools via MCP servers:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    instruction: You are a helpful assistant with web search capabilities.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo  # Containerized MCP server
      - type: mcp
        command: rust-mcp-filesystem
        args: ["--allow-write", "."]
        tools: ["read_file", "write_file"]  # Optional: specific tools only
```

Get started with the [Docker MCP Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/).

## Multi-Agent Teams

```yaml
agents:
  root:
    model: claude
    description: Main coordinator that delegates tasks
    instruction: |
      You are the root coordinator. Break down requests and delegate to specialists.
    sub_agents: ["helper"]

  helper:
    model: claude
    description: Assistant for specific tasks
    instruction: Complete tasks assigned by the root agent.

models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

More examples in the [examples directory](/examples/README.md).

## RAG (Retrieval-Augmented Generation)

Give agents access to your documents:

```yaml
models:
  embedder:
    provider: openai
    model: text-embedding-3-small

rag:
  knowledge_base:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: embedder
        threshold: 0.5
    results:
      limit: 5

agents:
  root:
    model: openai/gpt-4o
    instruction: Use the knowledge base to answer questions.
    rag: [knowledge_base]
```

See [RAG documentation](docs/USAGE.md#rag-configuration) for hybrid search, reranking, and more.

## Generate Agents with AI

```bash
cagent new
# Follow the prompts to describe your agent
```

Options: `--model provider/modelname`, `--max-tokens`, `--max-iterations`

## Share Agents via Docker Hub

```bash
# Push an agent
cagent push ./agent.yaml namespace/reponame

# Pull an agent
cagent pull creek/pirate

# Run from registry
cagent run creek/pirate
```

## Expose Agents as MCP Tools

```bash
cagent mcp ./examples/dev-team.yaml
```

See [MCP Mode documentation](./docs/MCP-MODE.md) for details.

## Documentation

- [Usage Guide](/docs/USAGE.md) – Detailed configuration options
- [Examples](/examples/README.md) – Basic, advanced, and multi-agent examples
- [MCP Mode](/docs/MCP-MODE.md) – Exposing agents as MCP tools
- [Contributing](/docs/CONTRIBUTING.md) – Build from source and contribute
- [Telemetry](/docs/TELEMETRY.md) – Anonymous usage tracking details

## Feedback

Join us on [Slack](https://dockercommunity.slack.com/archives/C09DASHHRU4)!
