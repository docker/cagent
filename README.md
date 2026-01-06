<div align="center">

# 🤖 cagent

### Build AI agent teams that actually get things done

[![GitHub release](https://img.shields.io/github/v/release/docker/cagent?style=flat-square)](https://github.com/docker/cagent/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/docker/cagent?style=flat-square)](https://goreportcard.com/report/github.com/docker/cagent)

[Getting Started](#-quick-start) •
[Examples](./examples/README.md) •
[Documentation](./docs/USAGE.md) •
[Contributing](./docs/CONTRIBUTING.md)

![cagent in action](docs/demo.gif)

</div>

---

**cagent** is a multi-agent runtime that lets you create teams of AI agents with specialized capabilities. Each agent can have its own tools, knowledge, and expertise—and they collaborate to solve complex problems.

```bash
brew install cagent && cagent run creek/pirate
```

> ⚠️ **Active Development** — Breaking changes may occur between releases

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| **Multi-Agent Teams** | Create specialized agents that collaborate and delegate tasks |
| **MCP Tool Support** | Connect to any MCP server for external tools and APIs |
| **RAG Built-in** | Vector search, BM25, hybrid retrieval with result fusion |
| **YAML Config** | Declarative, version-controllable agent definitions |
| **Multiple Providers** | OpenAI, Anthropic, Gemini, xAI, Mistral, Nebius, and local models via Docker Model Runner |
| **Push & Pull** | Share agents via Docker Hub as OCI artifacts |
| **Agent as MCP** | Expose your agents as MCP tools for other clients |

---

## 🚀 Quick Start

### Installation

<details>
<summary><b>Homebrew (macOS/Linux)</b></summary>

```bash
brew install cagent
```
</details>

<details>
<summary><b>Binary Releases</b></summary>

Download from [GitHub Releases](https://github.com/docker/cagent/releases) for Windows, macOS, or Linux.

```bash
chmod +x cagent-*  # Make executable on macOS/Linux
```
</details>

### Set API Keys

```bash
export OPENAI_API_KEY=sk-...      # OpenAI
export ANTHROPIC_API_KEY=sk-...   # Anthropic
export GOOGLE_API_KEY=...         # Gemini
export XAI_API_KEY=...            # xAI
export MISTRAL_API_KEY=...        # Mistral
export NEBIUS_API_KEY=...         # Nebius
```

### Your First Agent

Create `my_agent.yaml`:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
```

```bash
cagent run my_agent.yaml
```

💡 **Explore more examples** in the [examples directory](./examples/README.md)

---

## 🔧 Adding Tools with MCP

cagent supports [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) servers (`stdio`, `http`, `sse`), giving your agents access to external tools and services.

### Docker MCP Catalog

The easiest way to add tools—use containerized MCP servers via the [Docker MCP Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/):

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    instruction: You are a helpful assistant with web search capabilities.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
```

### Standard MCP Servers

Use any MCP server directly:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    instruction: You are a helpful assistant. Write your results to disk.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
      - type: mcp
        command: rust-mcp-filesystem
        args: ["--allow-write", "."]
        tools: ["read_file", "write_file"]  # Optional: filter tools
```

📖 See [Tool Configuration](./docs/USAGE.md#tool-configuration) for more details

---

## 👥 Multi-Agent Teams

Create teams of specialized agents that collaborate:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-0
    description: Main coordinator that delegates tasks
    instruction: |
      You are the coordinator. Break down complex requests
      and delegate to specialists as needed.
    sub_agents: ["researcher", "writer"]

  researcher:
    model: anthropic/claude-sonnet-4-0
    description: Research specialist for gathering information
    instruction: You gather and analyze information thoroughly.

  writer:
    model: anthropic/claude-sonnet-4-0
    description: Writing specialist for creating content
    instruction: You write clear, engaging content.
```

Browse [multi-agent examples](https://github.com/docker/cagent/tree/main/examples#multi-agent-configurations) for more patterns.

---

## 📚 RAG (Retrieval-Augmented Generation)

Give your agents access to your documents:

```yaml
models:
  embedder:
    provider: openai
    model: text-embedding-3-small

rag:
  knowledge_base:
    docs: [./documents, ./pdfs]
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

<details>
<summary><b>Hybrid Retrieval (Embeddings + BM25)</b></summary>

Combine semantic and keyword search:

```yaml
rag:
  hybrid_search:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: embedder
        limit: 20
      - type: bm25
        k1: 1.5
        b: 0.75
        limit: 15
    results:
      fusion:
        strategy: rrf
        k: 60
      limit: 5
```
</details>

<details>
<summary><b>Result Reranking</b></summary>

Re-score results for improved relevance:

```yaml
rag:
  knowledge_base:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: embedder
        limit: 20
    results:
      reranking:
        model: openai/gpt-4.1-mini
        top_k: 10
        threshold: 0.3
      limit: 5
```
</details>

📖 See [RAG documentation](./docs/USAGE.md#rag-configuration) for complete details

---

## 🏃 Local Models with Docker Model Runner

Run models locally with zero setup:

```yaml
models:
  local:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      runtime_flags: ["--ngl=33"]
      speculative_draft_model: ai/qwen3:1B  # Optional: faster inference
```

Enable DMR in [Docker Desktop](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-desktop) or [Docker CE](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-engine).

---

## 🎨 Generate Agents with AI

Let cagent create agents for you:

```bash
cagent new
```

```
------- Welcome to cagent! -------
What should your agent/agent team do? (describe its purpose):
> I need an agent team that reviews PRs and suggests improvements...
```

Options:
- `--model openai/gpt-5` — Choose provider/model
- `--model dmr/ai/qwen3` — Use local model
- `--max-tokens 32000` — Override context limit

---

## 📤 Share Agents via Docker Hub

```bash
# Push an agent
cagent push ./my_agent.yaml myuser/my-agent

# Pull and run an agent
cagent pull creek/pirate
cagent run creek.yaml

# Or run directly from registry
cagent run creek/pirate
```

---

## 🔌 Expose Agents as MCP Tools

Let other MCP clients use your agents:

```bash
cagent mcp ./my_agent.yaml
```

Each agent becomes an MCP tool. Works with Claude Desktop, Claude Code, and other MCP clients.

📖 See [MCP Mode documentation](./docs/MCP-MODE.md) for setup instructions

---

## 📖 Documentation

| Resource | Description |
|----------|-------------|
| [Usage Guide](./docs/USAGE.md) | Complete configuration reference |
| [Examples](./examples/README.md) | Curated agent configurations |
| [MCP Mode](./docs/MCP-MODE.md) | Exposing agents as MCP tools |
| [Contributing](./docs/CONTRIBUTING.md) | Build from source, contribute |
| [Telemetry](./docs/TELEMETRY.md) | Anonymous usage data details |

---

## 🐕 Dogfooding

We use cagent to develop cagent:

```bash
cd cagent
cagent run ./golang_developer.yaml
```

This agent is an expert Go developer specialized in the cagent codebase. Use it to explore the code, fix issues, or implement features.

---

## 💬 Community

Questions? Feedback? Find us on [Slack](https://dockercommunity.slack.com/archives/C09DASHHRU4)
