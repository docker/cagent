## Development Commands

### Build and Development

- `task build` - Build the application binary
- `task test` - Run Go tests
- `task lint` - Run golangci-lint
- `task format` - Format code
- `task link` - Create symlink to ~/bin for easy access

### Running cagent

- `./bin/cagent run <config.yaml>` - Run agent with configuration (uses TUI by default)
- `./bin/cagent run <config.yaml> --tui=false` - Run in CLI mode
- `./bin/cagent run <config.yaml> -a <agent_name>` - Run specific agent
- `./bin/cagent exec <config.yaml>` - Execute agent without TUI

### Single Test Execution

- `go test ./pkg/specific/package` - Run tests for specific package
- `go test ./pkg/... -run TestSpecificFunction` - Run specific test function
- `go test -v ./...` - Run all tests with verbose output

## Architecture Overview

cagent is a multi-agent AI system with hierarchical agent structure and pluggable tool ecosystem via MCP (Model Context Protocol).

### Core Components

#### Agent System (`pkg/agent/`)

- **Agent struct**: Core abstraction with name, description, instruction, toolsets, models, and sub-agents
- **Hierarchical structure**: Root agents coordinate sub-agents for specialized tasks
- **Tool integration**: Agents have access to built-in tools (think, todo, memory, transfer_task) and external MCP tools
- **Multi-model support**: Agents can use different AI providers (OpenAI, Anthropic, Gemini, DMR)

#### Runtime System (`pkg/runtime/`)

- **Event-driven architecture**: Streaming responses for real-time interaction
- **Tool execution**: Handles tool calls and coordinates between agents and external tools
- **Session management**: Maintains conversation state and message history
- **Task delegation**: Routes tasks between agents using transfer_task tool
- **Remote runtime support**: Can connect to remote runtime servers

#### Configuration System (`pkg/config/`)

- **YAML-based configuration**: Declarative agent, model, and tool definitions
- **Agent properties**: name, model, description, instruction, sub_agents, toolsets, add_date, add_environment_info, code_mode_tools, max_iterations, num_history_items
- **Model providers**: openai, anthropic, gemini, dmr with configurable parameters
- **Tool configuration**: MCP tools (local stdio and remote), builtin tools (filesystem, shell, think, todo, memory, etc.)

#### Command Layer (`cmd/root/`)

- **Multiple interfaces**: CLI (`run.go`), TUI (default for `run` command), API (`api.go`)
- **Interactive commands**: `/exit`, `/reset`, `/eval`, `/usage`, `/compact` during sessions
- **Debug support**: `--debug` flag for detailed logging
- **Gateway mode**: SSE-based transport for external MCP clients like Claude Code

### Tool System (`pkg/tools/`)

#### Built-in Tools

- **think**: Step-by-step reasoning tool
- **todo**: Task list management
- **memory**: Persistent SQLite-based storage
- **filesystem**: File operations
- **shell**: Command execution
- **script**: Custom shell scripts
- **fetch**: HTTP requests

#### MCP Integration

- **Local MCP servers**: stdio-based tools via command execution
- **Remote MCP servers**: SSE/streamable transport for remote tools
- **Docker-based MCP**: Reference MCP servers from Docker images (e.g., `docker:github-official`)
- **Tool filtering**: Optional tool whitelisting per agent

### Key Patterns

#### Agent Configuration

```yaml
version: "2"

agents:
  root:
    model: model_ref # Can be inline like "openai/gpt-4o" or reference defined models
    description: purpose
    instruction: detailed_behavior
    sub_agents: [list]
    toolsets:
      - type: mcp
      - type: think
      - type: todo
      - type: memory
        path: ./path/to/db
      - ...

models:
  model_ref:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

#### Task Delegation Flow

1. User → Root Agent
2. Root Agent analyzes request
3. Routes to appropriate sub-agent via transfer_task
4. Sub-agent processes with specialized tools
5. Results flow back through hierarchy

#### Stream Processing

- Models return streaming responses
- Runtime processes chunks and tool calls
- Events emitted for real-time UI updates
- Tool execution integrated into stream flow

## Development Guidelines

### Testing

- Tests located alongside source files (`*_test.go`)
- Run `task test` to execute full test suite

#### Testing Best Practices

This project uses `github.com/stretchr/testify` for assertions.

In Go tests, always prefer `require` and `assert` from the `testify` package over manual error handling.

### Configuration Validation

- All agent references must exist in config
- Model references can be inline (e.g., `openai/gpt-4o`) or defined in models section
- Tool configurations validated at startup

### Adding New Features

- Follow existing patterns in `pkg/` directories
- Implement proper interfaces for providers and tools
- Add configuration support if needed
- Consider both CLI and TUI interface impacts, along with API server impacts

## Model Provider Configuration Examples

Models can be referenced inline (e.g., `openai/gpt-4o`) or defined explicitly:

### OpenAI

```yaml
models:
  gpt4:
    provider: openai
    model: gpt-4o
    temperature: 0.7
    max_tokens: 4000
```

### Anthropic

```yaml
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

### Gemini

```yaml
models:
  gemini:
    provider: google
    model: gemini-2.0-flash
    temperature: 0.5
```

### DMR

```yaml
models:
  dmr:
    provider: dmr
    model: ai/llama3.2
```

## Tool Configuration Examples

### Local MCP Server (stdio)

```yaml
toolsets:
  - type: mcp
    command: "python"
    args: ["-m", "mcp_server"]
    tools: ["specific_tool"] # optional filtering
    env:
      API_KEY: "value"
```

### Remote MCP Server (SSE)

```yaml
toolsets:
  - type: mcp
    remote:
      url: "http://localhost:8080/mcp"
      transport_type: "sse"
      headers:
        Authorization: "Bearer token"
```

### Docker-based MCP Server

```yaml
toolsets:
  - type: mcp
    ref: docker:github-official
    instruction: |
      Use these tools to help with GitHub tasks.
```

### Memory Tool with Custom Path

```yaml
toolsets:
  - type: memory
    path: "./agent_memory.db"
```

### Shell Tool

```yaml
toolsets:
  - type: shell
```

### Filesystem Tool

```yaml
toolsets:
  - type: filesystem
```

## Common Development Patterns

### Agent Hierarchy Example

```yaml
version: "2"

agents:
  root:
    model: anthropic/claude-sonnet-4-0
    description: "Main coordinator"
    sub_agents: ["researcher", "writer"]
    toolsets:
      - type: transfer_task
      - type: think

  researcher:
    model: openai/gpt-4o
    description: "Research specialist"
    toolsets:
      - type: mcp
        ref: docker:search-tools

  writer:
    model: anthropic/claude-sonnet-4-0
    description: "Writing specialist"
    toolsets:
      - type: filesystem
      - type: memory
        path: ./writer_memory.db
```

### Session Commands During CLI Usage

- `/exit` - End the session
- `/reset` - Clear session history
- `/usage` - Show token usage statistics
- `/compact` - Generate summary and compact session history
- `/eval` - Save evaluation data

## File Locations and Patterns

### Key Package Structure

- `pkg/agent/` - Core agent abstraction and management
- `pkg/runtime/` - Event-driven execution engine
- `pkg/tools/` - Built-in and MCP tool implementations
- `pkg/model/provider/` - AI provider implementations
- `pkg/session/` - Conversation state management
- `pkg/config/` - YAML configuration parsing and validation
- `pkg/gateway/` - MCP gateway/server implementation
- `pkg/tui/` - Terminal User Interface components
- `pkg/api/` - API server implementation

### Configuration File Locations

- `examples/` - Sample agent configurations
- Root directory - Main project configurations (`Taskfile.yml`, `go.mod`)

### Environment Variables

- `OPENAI_API_KEY` - OpenAI authentication
- `ANTHROPIC_API_KEY` - Anthropic authentication
- `GOOGLE_API_KEY` - Google/Gemini authentication
- `TELEMETRY_ENABLED` - Control telemetry (set to false to disable)
- `CAGENT_HIDE_TELEMETRY_BANNER` - Hide telemetry banner message
- `CAGENT_HIDE_FEEDBACK_LINK` - Hide feedback link

## Debugging and Troubleshooting

### Debug Mode

- Add `--debug` flag to any command for detailed logging
- Logs written to `~/.cagent/cagent.debug.log` by default
- Use `--log-file <path>` to specify custom log location
- Example: `./bin/cagent run config.yaml --debug`

### OpenTelemetry Tracing

- Add `--otel` flag to enable OpenTelemetry tracing
- Example: `./bin/cagent run config.yaml --otel`
