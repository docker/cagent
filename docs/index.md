---
layout: default
title: "docker-agent"
description: "Build, run, and share powerful AI agents with a declarative YAML config, rich tool ecosystem, and multi-agent orchestration — by Docker."
permalink: /
---

<div class="hero">
  <h1>docker-agent</h1>
  <p>Build, run, and share powerful AI agents with a declarative YAML config, rich tool ecosystem, and multi-agent orchestration — by Docker.</p>
  <div class="hero-buttons">
    <a href="{{ '/getting-started/introduction/' | relative_url }}" class="btn btn-primary">Get Started</a>
    <a href="https://github.com/docker/cagent" target="_blank" rel="noopener noreferrer" class="btn btn-secondary">GitHub →</a>
  </div>
</div>

<div class="features-grid">
  <div class="feature">
    <div class="feature-icon">🤖</div>
    <h3>Declarative Agents</h3>
    <p>Define agents in simple YAML — their model, tools, behavior, and relationships. No framework code required.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔧</div>
    <h3>Rich Tool Ecosystem</h3>
    <p>Built-in tools for filesystem, shell, memory, and more. Extend with any MCP server — local, remote, or Docker-based.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">👥</div>
    <h3>Multi-Agent Orchestration</h3>
    <p>Create teams of specialized agents that delegate tasks to each other automatically via a coordinator.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🧠</div>
    <h3>Multi-Model Support</h3>
    <p>Use OpenAI, Anthropic, Google Gemini, AWS Bedrock, Docker Model Runner, or bring your own provider.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">📦</div>
    <h3>Package &amp; Share</h3>
    <p>Push agents to any OCI-compatible registry. Pull and run them anywhere — just like container images.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🖥️</div>
    <h3>Multiple Interfaces</h3>
    <p>Beautiful TUI for interactive sessions, CLI for scripting, HTTP API for integrations, and MCP mode for interop.</p>
  </div>
</div>

## See It in Action

<div class="demo-container">
  <img src="demo.gif" alt="docker agent TUI demo showing an interactive agent session" loading="lazy">
</div>

## Quick Example

Create a file called `agent.yaml`:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-0
    description: A helpful coding assistant
    instruction: |
      You are an expert developer. Help users write clean,
      efficient code and follow best practices.
    toolsets:
      - type: filesystem
      - type: shell
      - type: think
```

Then run it:

```bash
# Launch the interactive TUI
docker agent run agent.yaml

# Or run a one-shot command
docker agent run --exec agent.yaml "Explain the code in main.go"
```

## Explore the Docs

<div class="cards">
<a class="card" href="{{ '/getting-started/introduction/' | relative_url }}">
    <div class="card-icon">🚀
</div>
    <h3>Getting Started</h3>
    <p>Learn what docker agent is and get your first agent running in minutes.</p>
  </a>
  <a class="card" href="{{ '/concepts/agents/' | relative_url }}">
    <div class="card-icon">💡</div>
    <h3>Core Concepts</h3>
    <p>Understand agents, models, tools, and multi-agent orchestration.</p>
  </a>
  <a class="card" href="{{ '/configuration/overview/' | relative_url }}">
    <div class="card-icon">⚙️</div>
    <h3>Configuration</h3>
    <p>Complete reference for agent, model, and tool configuration.</p>
  </a>
  <a class="card" href="{{ '/features/tui/' | relative_url }}">
    <div class="card-icon">✨</div>
    <h3>Features</h3>
    <p>TUI, CLI, MCP mode, RAG, Skills, and distribution.</p>
  </a>
  <a class="card" href="{{ '/providers/overview/' | relative_url }}">
    <div class="card-icon">🧠</div>
    <h3>Model Providers</h3>
    <p>OpenAI, Anthropic, Gemini, AWS Bedrock, Docker Model Runner, and custom providers.</p>
  </a>
  <a class="card" href="{{ '/community/contributing/' | relative_url }}">
    <div class="card-icon">🤝</div>
    <h3>Community</h3>
    <p>Contributing guidelines, troubleshooting, and more.</p>
  </a>
</div>
