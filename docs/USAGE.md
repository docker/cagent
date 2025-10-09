# Usage and configuration

This guide will help you get started with `cagent` and learn how to use its
powerful multi-agent system to accomplish various tasks.

## What is cagent?

`cagent` is a powerful, customizable multi-agent system that orchestrates AI
agents with specialized capabilities and tools. It features:

- **🏗️ Multi-tenant architecture** with client isolation and session management
- **🔧 Rich tool ecosystem** via Model Context Protocol (MCP) integration
- **🤖 Hierarchical agent system** with intelligent task delegation
- **🌐 Multiple interfaces** including CLI, TUI and API server
- **📦 Agent distribution** via Docker registry integration
- **🔒 Security-first design** with proper client scoping and resource isolation
- **⚡ Event-driven streaming** for real-time interactions
- **🧠 Multi-model support** (OpenAI, Anthropic, Gemini, [Docker Model Runner (DMR)](https://docs.docker.com/ai/model-runner/))


## Why?

After passing the last year+ building AI agents of various types, using a variety of software solutions and frameworks, we kept asking ourselves some of the same questions:

- How can we make building and running useful agentic systems less of a hassle?
- Most agents we build end up use many of the same building blocks. Can we re-use most of those building block and have declarative configurations for new agents?
- How can we package and share agents amongst each other as simply as possible without all the headaches?

We really think we're getting somewhere as we build out the primitives of `cagent` so, in keeping with our love for open-source software in general, we decided to **share it and build it in the open** to allow the community at large to make use of our work and contribute to the future of the project itself. 

## Running Agents

### Command Line Interface

cagent provides multiple interfaces and deployment modes:

```bash
# Terminal UI (TUI)
$ cagent run config.yaml
$ cagent run config.yaml -a agent_name    # Run a specific agent
$ cagent run config.yaml --debug          # Enable debug logging
$ cagent run config.yaml --yolo           # Auto-accept all the tool calls
$ cagent run config.yaml "First message"  # Start the conversation with the agent with a first message
$ cagent run config.yaml -c df            # Run with a named command from YAML

# Model Override Examples
$ cagent run config.yaml --model anthropic/claude-sonnet-4-0    # Override all agents to use Claude
$ cagent run config.yaml --model "agent1=openai/gpt-4o"         # Override specific agent
$ cagent run config.yaml --model "agent1=openai/gpt-4o,agent2=anthropic/claude-sonnet-4-0"  # Multiple overrides

# One off without TUI
$ cagent exec config.yaml                 # Run the agent once, with default instructions
$ cagent exec config.yaml "First message" # Run the agent once with instructions
$ cagent exec config.yaml --yolo          # Run the agent once and auto-accept all the tool calls

# API Server (HTTP REST API)
$ cagent api config.yaml
$ cagent api config.yaml --listen :8080

# Other commands
$ cagent new                          # Initialize new project
$ cagent new --model openai/gpt-5-mini --max-tokens 32000  # Override max tokens during generation
$ cagent eval config.yaml             # Run evaluations
$ cagent pull docker.io/user/agent    # Pull agent from registry
$ cagent push docker.io/user/agent    # Push agent to registry
```

### Interface-Specific Features

#### CLI Interactive Commands

During CLI sessions, you can use special commands:

| Command    | Description                                 |
|------------|---------------------------------------------|
| `/exit`    | Exit the program                            |
| `/reset`   | Clear conversation history                  |
| `/eval`    | Save current conversation for evaluation    |
| `/compact` | Compact conversation to lower context usage |

## 🔧 Configuration Reference

### Agent Properties

| Property               | Type         | Description                                                     | Required |
|------------------------|--------------|-----------------------------------------------------------------|----------|
| `name`                 | string       | Agent identifier                                                | ✓        |
| `model`                | string       | Model reference                                                 | ✓        |
| `description`          | string       | Agent purpose                                                   | ✓        |
| `instruction`          | string       | Detailed behavior instructions                                  | ✓        |
| `sub_agents`           | array        | List of sub-agent names                                         | ✗        |
| `toolsets`             | array        | Available tools                                                 | ✗        |
| `add_date`             | boolean      | Add current date to context                                     | ✗        |
| `add_environment_info` | boolean      | Add information about the environment (working dir, OS, git...) | ✗        |
| `max_iterations`       | int          | Specifies how many times the agent can loop when using tools    | ✗        |
| `commands`             | object/array | Named prompts for quick-start commands (used with `--command`)  | ✗        |

#### Example

```yaml
agents:
  agent_name:
    model: string # Model reference
    description: string # Agent purpose
    instruction: string # Detailed behavior instructions
    tools: [] # Available tools (optional)
    sub_agents: [] # Sub-agent names (optional)
    add_date: boolean # Add current date to context (optional)
    add_environment_info: boolean # Add information about the environment (working dir, OS, git...) (optional)
    max_iterations: int # How many times this agent can loop when calling tools (optional, default = unlimited)
    commands: # Either mapping or list of singleton maps
      df: "check how much free space i have on my disk"
      ls: "list the files in the current directory"
```

### Running with named commands

- Use `--command` (or `-c`) to send a predefined prompt from the agent config as the first message.
- Example YAML forms supported:

```yaml
commands:
  df: "check how much free space i have on my disk"
  ls: "list the files in the current directory"
```

```yaml
commands:
  - df: "check how much free space i have on my disk"
  - ls: "list the files in the current directory"
```

Run:

```bash
cagent run ./agent.yaml -c df
cagent run ./agent.yaml --command ls
```

### Model Properties

| Property            | Type       | Description                                                           | Required |
|---------------------|------------|-----------------------------------------------------------------------|----------|
| `provider`          | string     | Provider: `openai`, `anthropic`, `dmr`                                | ✓        |
| `model`             | string     | Model name (e.g., `gpt-4o`, `claude-sonnet-4-0`)                      | ✓        |
| `temperature`       | float      | Randomness (0.0-1.0)                                                  | ✗        |
| `max_tokens`        | integer    | Response length limit                                                 | ✗        |
| `top_p`             | float      | Nucleus sampling (0.0-1.0)                                            | ✗        |
| `frequency_penalty` | float      | Repetition penalty (0.0-2.0)                                          | ✗        |
| `presence_penalty`  | float      | Topic repetition penalty (0.0-2.0)                                    | ✗        |
| `base_url`          | string     | Custom API endpoint                                                   | ✗        |
| `thinking_budget`   | string/int | Reasoning effort — OpenAI: effort string, Anthropic: token budget int | ✗        |

#### Example

```yaml
models:
  model_name:
    provider: string # Provider: openai, anthropic, google, dmr
    model: string # Model name: gpt-4o, claude-3-5-sonnet-latest, gemini-2.5-flash, qwen3:4B, ...
    temperature: float # Randomness (0.0-1.0)
    max_tokens: integer # Response length limit
    top_p: float # Nucleus sampling (0.0-1.0)
    frequency_penalty: float # Repetition penalty (0.0-2.0)
    presence_penalty: float # Topic repetition penalty (0.0-2.0)
    parallel_tool_calls: boolean
    thinking_budget: string|integer # OpenAI: effort level string; Anthropic: integer token budget
```

### Reasoning Effort (thinking_budget)

Determine how much the model should think by setting the `thinking_budget`

- **OpenAI**: use effort levels — `minimal`, `low`, `medium`, `high`
- **Anthropic**: set an integer token budget. Minimum is 1024; range is 1024–32768; must be strictly less than `max_tokens`. When set, cagent uses Anthropic's Beta Messages API with interleaved thinking enabled.

Examples (OpenAI):

```yaml
models:
  openai:
    provider: openai
    model: gpt-5-mini
    thinking_budget: low

agents:
  root:
    model: openai
    instruction: you are a helpful assistant
```

Examples (Anthropic):

```yaml
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
    thinking_budget: 1024

agents:
  root:
    model: claude
    instruction: you are a helpful assistant that doesn't think very much
```

#### Interleaved Thinking (Anthropic)

Anthropic's interleaved thinking feature uses the Beta Messages API to provide tool calling during model reasoning. You can control this behavior using the `interleaved_thinking` provider option:

```yaml
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
    thinking_budget: 8192  # Optional: defaults to 16384 when interleaved thinking is enabled
    provider_opts:
      interleaved_thinking: true   # Enable interleaved thinking (default: false)
```

Notes:

- If an invalid OpenAI effort value is set, the request will fail with a clear error
- For Anthropic, values < 1024 or ≥ `max_tokens` are ignored (warning logged)
- When `interleaved_thinking` is enabled, cagent uses Anthropic's Beta Messages API with a default thinking budget of 16384 tokens if not specified
- For unsupported providers, `thinking_budget` has no effect
- Debug logs include the applied effort (e.g., "OpenAI request using thinking_budget", "Anthropic Beta API using thinking_budget")

See `examples/thinking_budget.yaml` for a complete runnable demo.

#### Model Examples

> ⚠️ **NOTE** ⚠️  
> **More model names can be found [here](https://modelname.ai/)**

```yaml

# OpenAI
models:
  openai:
    provider: openai
    model: gpt-5-mini

# Anthropic
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0

# Gemini
models:
  gemini:
    provider: google
    model: gemini-2.5-flash

# Docker Model Runner (DMR)
models:
  qwen:
    provider: dmr
    model: ai/qwen3
```

#### DMR (Docker Model Runner) provider usage

If `base_url` is omitted, cagent will use `http://localhost:12434/engines/llama.cpp/v1` by default

You can pass DMR runtime (e.g. llama.cpp) options using  
```
models:
  provider: dmr
  provider_opts: 
    runtime_flags: 
```  
The context length is taken from `max_tokens` at the model level:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    # base_url: omitted -> auto-discovery via Docker Model plugin
    provider_opts:
      runtime_flags: ["--ngl=33", "--top-p=0.9"]
```

`runtime_flags` also accepts a single string with comma or space separation:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      runtime_flags: "--ngl=33 --top-p=0.9"
```

Explicit `base_url` example with multiline runtime_flags string:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    base_url: http://127.0.0.1:12434/engines/llama.cpp/v1
    provider_opts:
      runtime_flags: |
        --ngl=33
        --top-p=0.9
```

Requirements and notes:

- Docker Model plugin must be available for auto-configure/auto-discovery
  - Verify with: `docker model status --json`
- Configuration is best-effort; failures fall back to the default base URL
- `provider_opts` currently apply to `dmr` and `anthropic` providers
- `runtime_flags` are passed after `--` to the inference runtime (e.g., llama.cpp)

Parameter mapping and precedence (DMR):

- `ModelConfig` fields are translated into engine-specific runtime flags. For e.g. with the `llama.cpp` backend:
  - `temperature` → `--temp`
  - `top_p` → `--top-p`
  - `frequency_penalty` → `--frequency-penalty`
  - `presence_penalty` → `--presence-penalty`
  ...
- `provider_opts.runtime_flags` always take priority over derived flags on conflict. When a conflict is detected, cagent logs a warning indicating the overridden flag. `max_tokens` is the only exception for now

Examples:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    temperature: 0.5            # derives --temp 0.5
    top_p: 0.9                  # derives --top-p 0.9
    max_tokens: 8192            # sets --context-size=8192
    provider_opts:
      runtime_flags: ["--temp", "0.7", "--threads", "8"]  # overrides derived --temp, sets --threads
```

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    provider_opts:
      runtime_flags: "--ngl=33 --repeat-penalty=1.2"  # string accepted as well
```

Troubleshooting:

- Plugin not found: cagent will log a debug message and use the default base URL
- Endpoint empty in status: ensure the Model Runner is running, or set `base_url` manually
- Flag parsing: if using a single string, quote properly in YAML; you can also use a list


### Alloy models

"Alloy models" essentially means using more than one model in the same chat context. Not at the same time, but "randomly" throughout the conversation to try to take advantage of the strong points of each model.

More information on the idea can be found [here](https://xbow.com/blog/alloy-agents)

To have an agent use an alloy model, you can define more than one model in the `model` field, separated by commas.

Example:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-0,openai/gpt-5-mini
    ...
```

### Tool Configuration

### Available MCP Tools

Common MCP tools include:

- **Filesystem**: Read/write files
- **Shell**: Execute shell commands
- **Database**: Query databases
- **Web**: Make HTTP requests
- **Git**: Version control operations
- **Browser**: Web browsing and automation
- **Code**: Programming language specific tools
- **API**: REST API integration tools

### Configuring MCP Tools

**Local (stdio) MCP Server:**

```yaml

toolsets:
  - type: mcp # Model Context Protocol
    command: string # Command to execute
    args: [] # Command arguments
    tools: [] # Optional: List of specific tools to enable
    env: [] # Environment variables for this tool
    env_file: [] # Environment variable files
```

Example:

``` yaml
toolsets:
  - type: mcp
    command: rust-mcp-filesystem
    args: ["--allow-write", "."]
    tools: ["read_file", "write_file"] # Optional: specific tools only
    env:
      - "RUST_LOG=debug"
```

**Remote (SSE) MCP Server:**

```yaml
toolsets:
  - type: mcp # Model Context Protocol
    remote:
      url: string # Base URL to connect to
      transport_type: string # Type of MCP transport (sse or streamable)
      headers:
        key: value # HTTP headers. Mainly used for auth
    tools: [] # Optional: List of specific tools to enable
```

Example:

```yaml
toolsets:
  - type: mcp
    remote:
      url: "https://mcp-server.example.com"
      transport_type: "sse"
      headers:
        Authorization: "Bearer your-token-here"
    tools: ["search_web", "fetch_url"]
```

### Using tools via the Docker MCP Gateway

We recommend running containerized MCP tools, for security and resource isolation.
Under the hood, `cagent` will run them with the [Docker MCP Gateway](https://github.com/docker/mcp-gateway)
so that all the tools in the `Docker MCP Catalog` can be accessed through a single endpoint.

In this example, lets configure `duckduckgo` to give our agents the ability to search the web:

```yaml
toolsets:
  - type: mcp
    ref: docker:duckduckgo
```

### Installing MCP Tools

Example installation of local tools with `cargo` or `npm`:

```bash
# Install Rust-based MCP filesystem tool
cargo install rust-mcp-filesystem

# Install other popular MCP tools
npm install -g @modelcontextprotocol/server-filesystem
npm install -g @modelcontextprotocol/server-git
npm install -g @modelcontextprotocol/server-web
```

## Built-in Tools

Included in `cagent` are a series of built-in tools that can greatly enhance the capabilities of your agents without needing to configure any external MCP tools.  

**Configuration example**

```yaml
toolsets:
  - type: filesystem # Grants the agent filesystem access
  - type: think # Enables the think tool
  - type: todo # Enable the todo list tool
    shared: boolean # Should the todo list be shared between agents (optional)
  - type: memory # Allows the agent to store memories to a local sqlite db
    path: ./mem.db # Path to the sqlite database for memory storage (optional)
```

Let's go into a bit more detail about the built-in tools that agents can use:

### Think Tool

The think tool allows agents to reason through problems step by step:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: think
```

### Todo Tool

The todo tool helps agents manage task lists:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: todo
```

### Memory Tool

The memory tool provides persistent storage:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: memory
        path: "./agent_memory.db"
```

### Task Transfer Tool

All agents automatically have access to the task transfer tool, which allows
them to delegate tasks to other agents:

```
transfer_task(agent="developer", task="Create a login form", expected_output="HTML and CSS code")
```

## Examples

### Development Team

A complete development team with specialized roles:

```yaml
agents:
  root:
    model: claude
    description: Technical lead coordinating development
    instruction: |
      You are a technical lead managing a development team.
      Coordinate tasks between developers and ensure quality.
    sub_agents: [developer, reviewer, tester]

  developer:
    model: claude
    description: Expert software developer
    instruction: |
      You are an expert developer. Write clean, efficient code
      and follow best practices.
    toolsets:
      - type: filesystem
      - type: shell
      - type: think

  reviewer:
    model: gpt4
    description: Code review specialist
    instruction: |
      You are a code review expert. Focus on code quality,
      security, and maintainability.
    toolsets:
      - type: filesystem

  tester:
    model: gpt4
    description: Quality assurance engineer
    instruction: |
      You are a QA engineer. Write tests and ensure
      software quality.
    toolsets:
      - type: shell
      - type: todo

models:
  gpt4:
    provider: openai
    model: gpt-4o

  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

### Research Assistant

A research-focused agent with web access:

```yaml
agents:
  root:
    model: claude
    description: Research assistant with web access
    instruction: |
      You are a research assistant. Help users find information,
      analyze data, and provide insights.
    toolsets:
      - type: mcp
        command: mcp-web-search
        args: ["--provider", "duckduckgo"]
      - type: todo
      - type: memory
        path: "./research_memory.db"

models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

## Advanced Features

### Agent Store and Distribution

cagent supports distributing via, and running agents from, Docker registries:

```bash
# Pull an agent from a registry
./bin/cagent pull docker.io/username/my-agent:latest

# Push your agent to a registry
./bin/cagent push docker.io/username/my-agent:latest

# Run an agent directly from an image reference
./bin/cagent run docker.io/username/my-agent:latest
```

**Agent References:**

- File agents: `my-agent.yaml` (relative path)
- Store agents: `docker.io/username/my-agent:latest` (full Docker reference)

## Troubleshooting

### Common Issues

**Agent not responding:**

- Check API keys are set correctly
- Verify model name matches provider
- Check network connectivity

**Tool errors:**

- Ensure MCP tools are installed and accessible
- Check file permissions for filesystem tools
- Verify tool arguments and command paths
- Test MCP tools independently before integration
- Check tool lifecycle (start/stop) in debug logs

**Configuration errors:**

- Validate YAML syntax
- Check all referenced agents exist
- Ensure all models are defined
- Verify toolset configurations
- Check agent hierarchy (sub_agents references)

**Session and connectivity issues:**

- Verify port availability for MCP server modes
- Test MCP endpoint accessibility (curl test)
- Verify client isolation in multi-tenant scenarios
- Check session timeouts and limits

**Performance issues:**

- Monitor memory usage with multiple concurrent sessions
- Check for tool resource leaks
- Verify proper session cleanup
- Monitor streaming response performance

### Debug Mode

Enable debug logging for detailed information:

```bash
# CLI mode
./bin/cagent run config.yaml --debug
```

### Log Analysis

Check logs for:

- API call errors and rate limiting
- Tool execution failures and timeouts
- Configuration validation issues
- Network connectivity problems
- MCP protocol handshake issues
- Session creation and cleanup events
- Client isolation boundary violations

### Agent Store Issues

```bash
# Test Docker registry connectivity
docker pull docker.io/username/agent:latest

# Verify agent content
./bin/cagent pull docker.io/username/agent:latest
```

## Integration Examples

### Custom Memory Strategies

Implement persistent memory across sessions:

```yaml
agents:
  researcher:
    model: claude
    instruction: |
      You are a research assistant with persistent memory.
      Remember important findings and reference previous research.
    toolsets:
      - type: memory
        path: ./research_memory.db
```

### Multi-Model Teams

```yaml
models:
  # Local model for fast responses
  claude_local:
    provider: anthropic
    model: claude-sonnet-4-0
    temperature: 0.2

  gpt4:
    provider: openai
    model: gpt-4o
    temperature: 0.1

  # Creative model for content generation
  gpt4_creative:
    provider: openai
    model: gpt-4o
    temperature: 0.8

agents:
  analyst:
    model: claude_local
    description: Fast analysis and reasoning

  coder:
    model: gpt4
    description: not very creative developer

  writer:
    model: gpt4_creative
    description: Creative content generation
```

This guide should help you get started with cagent and build powerful
multi-agent systems.
