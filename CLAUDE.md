# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Build and Development

- `task build` - Build the application binary 
- `task test` - Run Go tests
- `task lint` - Run golangci-lint
- `task link` - Create symlink to ~/bin for easy access

### Docker Operations

- `task build-image` - Build Docker image
- `task build-local` - Build binaries for local platform using Docker
- `task cross` - Build cross-platform binaries using Docker

### Running cagent

- `./bin/cagent run <config.yaml>` - Run agent with configuration
- `./bin/cagent run <config.yaml> -a <agent_name>` - Run specific agent
- `./bin/cagent init` - Initialize new project

### Single Test Execution

- `go test ./pkg/specific/package` - Run tests for specific package
- `go test ./pkg/... -run TestSpecificFunction` - Run specific test function
- `go test -v ./...` - Run all tests with verbose output

## Architecture Overview

cagent is a multi-agent AI system with hierarchical agent structure and pluggable tool ecosystem via MCP (Model Context Protocol).

### Core Components

#### ServiceCore Layer (`pkg/servicecore/`)

- **Multi-tenant architecture**: Client-isolated operations ensuring security between different users
- **Transport-agnostic design**: Core business logic independent of MCP/HTTP transport specifics
- **Agent resolution**: File-based and Docker store-based agent discovery with explicit reference formatting
- **Session management**: Per-client session lifecycle with proper resource cleanup
- **Security-first design**: All operations require client ID scoping, preventing cross-client data access

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

#### Configuration System (`pkg/config/`)

- **YAML-based configuration**: Declarative agent, model, and tool definitions
- **Agent properties**: name, model, description, instruction, sub_agents, toolsets, think, todo, memory, add_date, add_environment_info
- **Model providers**: openai, anthropic, dmr with configurable parameters
- **Tool configuration**: MCP tools (local stdio and remote), builtin tools (filesystem, shell)

#### Command Layer (`cmd/root/`)

- **Multiple interfaces**: CLI (`run.go`), TUI (`tui.go`), API (`api.go`)
- **Interactive commands**: `/exit`, `/reset`, `/eval` during sessions
- **Debug support**: `--debug` flag for detailed logging
- **MCP server mode**: SSE-based transport for external MCP clients like Claude Code

#### MCP Server (`pkg/mcpserver/`)

- **Protocol compliance**: Full MCP specification implementation with SSE transport
- **Tool handlers**: Agent listing, invocation, session management, and Docker image operations
- **Client isolation**: Per-client contexts preventing cross-client interference
- **Structured responses**: Explicit agent_ref formatting for file vs store-based agents

### Tool System (`pkg/tools/`)

#### Built-in Tools

- **think**: Step-by-step reasoning tool
- **todo**: Task list management
- **memory**: Persistent SQLite-based storage
- **transfer_task**: Agent-to-agent task delegation
- **filesystem**: File operations
- **shell**: Command execution

#### MCP Integration

- **Local MCP servers**: stdio-based tools via command execution
- **Remote MCP servers**: SSE/streamable transport for remote tools
- **Tool filtering**: Optional tool whitelisting per agent

### Key Patterns

#### Agent Configuration

```yaml
agents:
  root:
    model: model_ref
    description: purpose
    instruction: detailed_behavior
    sub_agents: [list]
    toolsets:
      - type: mcp
      - type: think
      - type: todo
      - type: memory
        path: { path: string }
      - ...
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

### Configuration Validation

- All agent references must exist in config
- Model references must be defined
- Tool configurations validated at startup

### Adding New Features

- Follow existing patterns in `pkg/` directories
- Implement proper interfaces for providers and tools
- Add configuration support if needed
- Consider both CLI and TUI interface impacts, along with API server impacts

### Key Patterns

#### Agent Reference Formatting

- **File agents**: Use relative path from agents directory (e.g., `agent.yaml`)
- **Store agents**: Use full Docker image reference with tag (e.g., `user/agent:latest`)
- **Explicit agent_ref field**: MCP responses include unambiguous agent reference for tool calls

#### MCP vs HTTP API

- **MCP Server**: Designed for multi-tenant with client isolation, secure by design, recommended for external integrations
- **HTTP API**: Legacy single-tenant mode, no client isolation, used for backward compatibility
- **ServiceCore abstraction**: Shared business logic between both transport layers

#### Current Multi-Tenant Limitation

- **ServiceCore layer**: Fully supports multi-tenant operation with client isolation
- **MCP implementation**: Client ID extraction from MCP context is not yet implemented
- **Current behavior**: All MCP clients use `DEFAULT_CLIENT_ID` ("\_\_global"), making them effectively share sessions
- **Impact**: Multiple MCP clients can see and interact with each other's agent sessions
- **Recommendation**: Use single MCP client per cagent instance until full multi-tenant support is implemented

## Model Provider Configuration Examples

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
    provider: gemini
    model: gemini-2.0-flash
    temperature: 0.5
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
      - "API_KEY=value"
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

### Memory Tool with Custom Path

```yaml
toolsets:
  - type: memory
    path: "./agent_memory.db"
```

## Common Development Patterns

### Agent Hierarchy Example

```yaml
agents:
  root:
    model: claude
    description: "Main coordinator"
    sub_agents: ["researcher", "writer"]
    toolsets:
      - type: transfer_task
      - type: think

  researcher:
    model: gpt4
    description: "Research specialist"
    toolsets:
      - type: mcp
        command: "web_search_tool"

  writer:
    model: claude
    description: "Writing specialist"
    toolsets:
      - type: filesystem
      - type: memory
```

### Session Commands During CLI Usage

- `/exit` - End the session
- `/reset` - Clear session history
- `/eval <expression>` - Evaluate expression (debug mode)

## File Locations and Patterns

### Key Package Structure

- `pkg/servicecore/` - Multi-tenant business logic layer
- `pkg/agent/` - Core agent abstraction and management
- `pkg/runtime/` - Event-driven execution engine
- `pkg/tools/` - Built-in and MCP tool implementations
- `pkg/model/provider/` - AI provider implementations
- `pkg/session/` - Conversation state management
- `pkg/config/` - YAML configuration parsing and validation
- `pkg/mcpserver/` - MCP protocol server implementation

### Configuration File Locations

- `examples/config/` - Sample agent configurations
- Root directory - Main project configurations (`Taskfile.yml`, `go.mod`)

### Environment Variables

- `OPENAI_API_KEY` - OpenAI authentication
- `ANTHROPIC_API_KEY` - Anthropic authentication
- `GOOGLE_API_KEY` - Google/Gemini authentication
- `MCP_SSE_ENDPOINT` - Override MCP test endpoint

## Debugging and Troubleshooting

### Debug Mode

- Add `--debug` flag to any command for detailed logging
- Example: `./bin/cagent run config.yaml --debug`

### Common Issues

- **Agent not found**: Check agent name matches config file agent definitions
- **Tool startup failures**: Verify MCP tool commands and dependencies are available
- **Multi-tenant sessions**: Remember all MCP clients currently share sessions
