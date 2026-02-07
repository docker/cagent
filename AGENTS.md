## Development Commands

### Build and Development

- `task build` - Build the application binary (outputs to `./bin/cagent`)
- `task test` - Run Go tests (clears API keys to ensure deterministic tests)
- `task lint` - Run golangci-lint (uses `.golangci.yml` configuration)
- `task format` - Format code using golangci-lint fmt
- `task dev` - Run lint, test, and build in sequence

### Docker and Cross-Platform Builds

- `task build-local` - Build binary for local platform using Docker Buildx
- `task cross` - Build binaries for multiple platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64)
- `task build-image` - Build Docker image tagged as `docker/cagent`
- `task push-image` - Build and push multi-platform Docker image to registry

### Running cagent

- `./bin/cagent run <config.yaml>` - Run agent with configuration (launches TUI by default)
- `./bin/cagent run <config.yaml> -a <agent_name>` - Run specific agent from multi-agent config
- `./bin/cagent run agentcatalog/pirate` - Run agent directly from OCI registry
- `./bin/cagent exec <config.yaml>` - Execute agent without TUI (non-interactive)
- `./bin/cagent new` - Generate new agent configuration interactively
- `./bin/cagent new --model openai/gpt-5` - Generate with specific model
- `./bin/cagent push ./agent.yaml namespace/repo` - Push agent to OCI registry
- `./bin/cagent pull namespace/repo` - Pull agent from OCI registry
- `./bin/cagent mcp ./agent.yaml` - Expose agents as MCP tools
- `./bin/cagent a2a <config.yaml>` - Start agent as A2A server
- `./bin/cagent api` - Start Docker `cagent` API server

### Debug and Development Flags

- `--debug` or `-d` - Enable debug logging (logs to `~/.cagent/cagent.debug.log`)
- `--log-file <path>` - Specify custom debug log location
- `--otel` or `-o` - Enable OpenTelemetry tracing
- Example: `./bin/cagent run config.yaml --debug --log-file ./debug.log`

### Single Test Execution

- `go test ./pkg/specific/package` - Run tests for specific package
- `go test ./pkg/... -run TestSpecificFunction` - Run specific test function
- `go test -v ./...` - Run all tests with verbose output
- `go test -parallel 1 ./...` - Run tests serially (useful for debugging)

### Interactive Session Commands

During a `cagent run` session, you can use:
- `/new` - Clear session history and start fresh
- `/compact` - Generate summary and compact session history
- `/copy` - Copy the current conversation to the clipboard
- `/eval` - Save evaluation data
- `/exit` - Exit the session
- `/reset` - Reset the session
- `/usage` - Display token usage statistics

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
- E2E tests in `e2e/` directory
- Test fixtures and data in `testdata/` subdirectories

#### Testing Best Practices

This project uses `github.com/stretchr/testify` for assertions and mocking.

**Core Testing Patterns:**

1. **Always use `require` and `assert` from testify** - Never use manual error handling in tests
2. **Use `t.Helper()` in test helper functions** - Improves error reporting
3. **Use `t.Context()` for test contexts** - Never use `context.Background()` or `context.TODO()` (enforced by linter)
4. **Use `t.TempDir()` for temporary directories** - Never use `os.MkdirTemp()` (enforced by linter)
5. **Use `t.Setenv()` for environment variables** - Never use `os.Setenv()` (enforced by linter)
6. **Run tests in parallel when possible** - Use `t.Parallel()` for independent tests

**VCR Pattern for E2E Tests:**
```go
// Record/replay AI API interactions for deterministic tests
recorder, err := startRecordingAIProxy(ctx, t, "test_name")
require.NoError(t, err)
defer recorder.Stop()
// Test code that makes AI API calls
```
- Cassettes stored in `e2e/testdata/cassettes/`
- Uses `go-vcr.v4` for recording/playback
- Custom matcher normalizes tool call IDs

**Golden File Pattern:**
```go
// Compare test output against saved reference
import "gotest.tools/v3/golden"
golden.Assert(t, actualContent, "expected.golden")
```
- Golden files in `testdata/` directories
- Used for snapshot testing of complex outputs

**Mock Pattern:**
```go
type MockService struct {
    mock.Mock
}

func (m *MockService) Method(arg string) error {
    args := m.Called(arg)
    return args.Error(0)
}

// In test:
mockSvc := new(MockService)
mockSvc.On("Method", "input").Return(nil)
defer mockSvc.AssertExpectations(t)
```

**Table-Driven Tests:**
```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {"case1", "input1", "output1", false},
    {"case2", "input2", "", true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()
        got, err := Function(tt.input)
        if tt.wantErr {
            require.Error(t, err)
            return
        }
        require.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

### Configuration Validation

- All agent references must exist in config
- Model references can be inline (e.g., `openai/gpt-4o`) or defined in models section
- Tool configurations validated at startup
- Config versioning
- Environment variables not stored in configs - gathered dynamically at startup
- Missing required env vars (e.g., API keys) trigger startup errors

### Configuration Loading Process

1. **Version detection**: Parse `version` field from YAML
2. **Version-specific parsing**: Load config using appropriate version struct
3. **Migration**: Apply sequential upgrades (v0→v1→v2) via `UpgradeFrom()` methods
4. **Validation**: Check agent references, model configs, toolset constraints
5. **Env var gathering**: Dynamically collect required API keys and MCP tool secrets

**Key validation rules:**
- Agents must reference existing sub-agents
- Model provider must be valid (`openai`, `anthropic`, `google`, `dmr`, etc.)
- Toolset-specific fields validated (e.g., `path` only valid for `memory` toolsets)
- MCP tools preflight-checked for required environment variables

### Adding New Features

- Follow existing patterns in `pkg/` directories
- Implement proper interfaces for providers and tools
- Add configuration support if needed
- Consider both CLI and TUI interface impacts, along with API server impacts
- Add tests alongside implementation (`*_test.go`)
- Update `cagent-schema.json` if adding new config fields

### Code Style and Conventions

**Error Handling:**
```go
// Always wrap errors with context using fmt.Errorf with %w
if err != nil {
    return fmt.Errorf("failed to load agents: %w", err)
}

// For YAML errors, use formatted output
if err := yaml.Unmarshal(data, &raw); err != nil {
    return nil, fmt.Errorf("parsing config:\n%s", yaml.FormatError(err, true, true))
}

// Check context cancellation explicitly when relevant
if errors.Is(err, context.Canceled) {
    slog.Debug("Operation canceled", "component", name)
    return nil, err
}
```

**Context Usage:**
```go
// Always pass context as first parameter
func (r *Runtime) RunStream(ctx context.Context, sess *session.Session) <-chan Event

// Check context before expensive operations
if err := ctx.Err(); err != nil {
    return err
}

// Use WithoutCancel for operations that should persist beyond parent cancellation
ctx = context.WithoutCancel(ctx)
```

**Logging with slog:**
```go
// Use structured logging with key-value pairs
slog.Debug("Starting runtime stream", "agent", agentName, "session_id", sess.ID)
slog.Error("Operation failed", "component", name, "error", err)
slog.Warn("Non-fatal issue", "details", info)

// Group related log statements under subsystem prefixes when needed
slog.Debug("[Telemetry] Event tracked", "event", eventName)
```

**Struct Initialization:**
```go
// Use functional options pattern for constructors
func New(name string, opts ...Opt) *Agent {
    agent := &Agent{name: name}
    for _, opt := range opts {
        opt(agent)
    }
    return agent
}

type Opt func(*Agent)

func WithModel(model provider.Provider) Opt {
    return func(a *Agent) { a.models = append(a.models, model) }
}

// Use session builder pattern
sess := session.New(
    session.WithTitle("Task"),
    session.WithMaxIterations(maxIter),
    session.WithUserMessage(filename, input),
)
```

**Interface Design:**
```go
// Keep interfaces minimal and focused
type Runtime interface {
    CurrentAgentName() string
    RunStream(ctx context.Context, sess *session.Session) <-chan Event
    Run(ctx context.Context, sess *session.Session) ([]session.Message, error)
}

// Use embedding for interface composition
type StartableToolSet struct {
    tools.ToolSet
    started atomic.Bool
}
```

**Concurrency Patterns:**
```go
// Use atomic types for flags
var started atomic.Bool
if started.Load() {
    return
}
started.Store(true)

// Use buffered channels for event streaming
events := make(chan Event, 128)
go func() {
    defer close(events)
    // ... emit events
    events <- StreamStarted(sess.ID, agentName)
}()
return events
```

**Type Safety:**
```go
// Always check type assertions
if errEvent, ok := event.(*ErrorEvent); ok {
    return fmt.Errorf("%s", errEvent.Error)
}
```

### Linter Configuration

The project uses `golangci-lint` with strict rules (`.golangci.yml`):

**Forbidden patterns in tests:**
- `context.Background()` → use `t.Context()`
- `context.TODO()` → use `t.Context()`
- `os.MkdirTemp()` → use `t.TempDir()`
- `os.Setenv()` → use `t.Setenv()`
- `fmt.Print*()` → use testing or logging facilities

**Dependency rules:**
- Don't use `github.com/docker/cagent/internal` from `/pkg/`
- Don't use deprecated `gopkg.in/yaml.v3` → use `github.com/goccy/go-yaml`
- Don't use `testify` in production code (test files only)

**Enabled linters:**
- `gocritic`, `govet`, `staticcheck`, `revive` - code quality
- `errcheck`, `ineffassign`, `unused`, `unparam` - error and unused code detection
- `testifylint`, `ginkgolinter`, `thelper` - test quality
- `forbidigo` - pattern-based forbidden code detection
- `depguard` - dependency restrictions

**Code formatters:**
- `gofmt` - standard Go formatting (with `interface{}` → `any` rewrite)
- `gofumpt` - stricter formatting with extra rules
- `gci` - import ordering (standard, default, `github.com/docker/cagent`)

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


## Runtime Execution Flow

Understanding how Docekr `cagent` processes user input through to agent responses:

### Main Execution Loop

**Entry Point**: `Runtime.RunStream()` in `pkg/runtime/runtime.go`

1. **Tool Discovery**: Agent's tools loaded via `agent.Tools()`
   - Built-in tools (think, todo, memory, transfer_task)
   - MCP tools from configured toolsets
   - Tools mapped to handlers in runtime's `toolMap`

2. **Message Preparation**: Session messages retrieved with `sess.GetMessages()`
   - System messages (agent instruction)
   - User messages
   - Previous assistant/tool messages
   - Context limited by `num_history_items` config

3. **LLM Streaming**: Model called via `CreateChatCompletionStream()`
   - Streams response chunks in real-time
   - Parsed by `handleStream()` into events
   - Events emitted: `AgentChoice`, `ToolCall`, `StreamChunk`, etc.

4. **Tool Execution**: Tool calls processed by `processToolCalls()`
   - Built-in tools routed to runtime handlers
   - MCP tools routed to toolset implementations
   - Optional user confirmation flow (unless auto-approved)
   - Results added back to session as tool response messages

5. **Iteration**: Loop continues until:
   - Agent returns without tool calls (stopped)
   - Max iterations reached
   - Context canceled
   - Error occurred

### Tool Call Flow

**Built-in Tools** (`pkg/runtime/runtime.go`):
```go
// Runtime maintains toolMap for built-in tools
toolMap := map[string]ToolHandler{
    builtin.ToolNameTransferTask: r.handleTaskTransfer,
    // ...other built-in tools
}

// Tool execution:
handler, exists := toolMap[toolCall.Function.Name]
if exists {
    if sess.ToolsApproved || toolCall.Function.Name == builtin.ToolNameTransferTask {
        r.runAgentTool(ctx, handler, sess, toolCall, tool, events, a)
    } else {
        // Emit confirmation event and wait for user approval
        events <- ToolCallConfirmation(toolCall, tool, a.Name())
        confirmationType := <-r.resumeChan
    }
}
```

**MCP Tools** (`pkg/tools/mcp/`):
- Loaded from stdio commands or remote connections
- Executed via MCP protocol (`tools/call` method)
- Support elicitation (interactive prompts for missing data)

### Agent Delegation (transfer_task)

**Handler**: `handleTaskTransfer()` in `pkg/runtime/runtime.go`

1. Parse target agent name from tool call arguments
2. Validate agent exists in hierarchy
3. Create new session with task context
4. Switch `currentAgent` to target agent
5. Recursively call `RunStream()` with child session
6. Forward child events to parent stream
7. Return child's last message as tool call result
8. Restore parent agent as current

**Example delegation:**
```yaml
# User → Root Agent → Sub-Agent
User: "Research topic X"
Root: calls transfer_task(agent="researcher", task="Research topic X")
Researcher: performs research using MCP tools
Researcher: returns results
Root: receives results, responds to user
```

### Event Streaming Architecture

**Event Channel** (buffered, capacity 128):
```go
events := make(chan Event, 128)
go func() {
    defer close(events)
    // Emit events during execution
    events <- StreamStarted(sess.ID, agentName)
    events <- AgentChoice(content, agentName)
    events <- ToolCall(toolCall, agentName)
    events <- ToolCallResponse(result, agentName)
    events <- StreamStopped(sess.ID, agentName, stopped)
}()
return events
```

**Event Types** (`pkg/runtime/event.go`):
- `StreamStarted` - Runtime begins processing
- `AgentChoice` - Partial or complete agent response
- `ToolCall` - Agent requests tool execution
- `ToolCallConfirmation` - User approval required
- `ToolCallResponse` - Tool execution result
- `ErrorEvent` - Error occurred
- `StreamStopped` - Runtime completed/stopped

**Consumers**:
- TUI (`pkg/tui/`) - Renders events in terminal UI
- CLI (`cmd/root/run.go`) - Prints events to stdout
- API Server (`pkg/api/`) - Streams events over HTTP/SSE
- MCP Gateway (`pkg/gateway/`) - Translates to MCP protocol

### Session Management

**Session** (`pkg/session/`):
- Maintains conversation history (messages)
- Tracks current state (tool calls, iterations)
- Stores configuration (max iterations, title)
- Provides context for agents

**Message Types**:
- `SystemMessage` - Agent instruction/prompt
- `UserMessage` - User input
- `AssistantMessage` - Agent response (text + tool calls)
- `ToolMessage` - Tool execution result

### TUI Animation Coordination

All animated TUI components share a single tick stream via `pkg/tui/animation/`.

```go
// Init: register and maybe start tick
func (m *MyComponent) Init() tea.Cmd {
    return animation.StartTickIfFirst()
}

// Update: handle tick
if tick, ok := msg.(animation.TickMsg); ok {
    m.frame = tick.Frame
}

// When done: unregister
animation.Unregister()
```

**Rules:** Only call from `Init()`/`Update()`, never from `Cmd` goroutines. Always `Unregister()` when animation stops.

## File Locations and Patterns

### Key Package Structure

- `pkg/agent/` - Core agent abstraction and management
- `pkg/runtime/` - Event-driven execution engine
- `pkg/tools/` - Built-in and MCP tool implementations
  - `pkg/tools/builtin/` - Core tools (think, todo, memory, filesystem, shell, fetch, script)
  - `pkg/tools/mcp/` - MCP protocol implementation (stdio, remote, gateway)
  - `pkg/tools/codemode/` - Code execution tools
- `pkg/model/provider/` - AI provider implementations (OpenAI, Anthropic, Gemini, DMR, etc.)
- `pkg/session/` - Conversation state management
- `pkg/config/` - YAML configuration parsing and validation
  - `pkg/config/v0/`, `pkg/config/v1/`, `pkg/config/v2/` - Version-specific schemas
- `pkg/gateway/` - MCP gateway/server implementation
- `pkg/tui/` - Terminal User Interface components (Bubble Tea)
- `pkg/api/` - API server implementation (REST/SSE)
- `pkg/a2a/` - Agent-to-Agent protocol implementation
- `pkg/acp/` - Agent Client Protocol implementation
- `pkg/oci/` - OCI registry operations (push/pull agents)
- `pkg/environment/` - Environment variable handling
- `pkg/paths/` - Path utilities and resolution
- `pkg/telemetry/` - Usage telemetry
- `pkg/evaluation/` - Agent evaluation framework
- `cmd/root/` - CLI commands and subcommands

### Configuration File Locations

- `examples/` - Sample agent configurations organized by complexity
  - `examples/README.md` - Guide to example agents
  - `examples/basic_agent.yaml` - Minimal agent example
  - `examples/dev-team.yaml` - Multi-agent team example
  - `examples/eval/` - Evaluation configurations
- Root directory - Main project configurations (`Taskfile.yml`, `go.mod`, `.golangci.yml`)
- `.github/workflows/ci.yml` - CI/CD pipeline
- `cagent-schema.json` - JSON schema for agent configuration validation
- `golang_developer.yaml` - Dogfooding agent for Docker `cagent` development

### Environment Variables

**Model Provider API Keys:**
- `OPENAI_API_KEY` - OpenAI authentication
- `ANTHROPIC_API_KEY` - Anthropic authentication
- `GOOGLE_API_KEY` - Google/Gemini authentication
- `MISTRAL_API_KEY` - Mistral authentication
- `XAI_API_KEY` - xAI authentication
- `NEBIUS_API_KEY` - Nebius authentication

**Telemetry:**
- `TELEMETRY_ENABLED` - Control telemetry (set to `false` to disable)
- `CAGENT_HIDE_TELEMETRY_BANNER` - Hide telemetry banner message

**Testing:**
- Tests run with all API keys cleared to ensure deterministic behavior
- VCR cassettes used for E2E tests to replay AI API interactions

## Debugging and Troubleshooting

### Debug Mode

- Add `--debug` flag to any command for detailed logging
- Logs written to `~/.cagent/cagent.debug.log` by default
- Use `--log-file <path>` to specify custom log location
- Example: `./bin/cagent run config.yaml --debug`

### OpenTelemetry Tracing

- Add `--otel` flag to enable OpenTelemetry tracing
- Example: `./bin/cagent run config.yaml --otel`
- Traces include spans for runtime operations, tool calls, and model interactions

### Common Issues and Solutions

**Config Validation Errors:**
- Check agent references exist: all `sub_agents` must be defined in `agents` section
- Verify model provider names: must be one of `openai`, `anthropic`, `google`, `dmr`, `mistral`, etc.
- Check toolset-specific fields: e.g., `path` only valid for `memory` toolsets
- Review error messages - YAML parsing errors show line numbers and context

**Missing API Keys:**
- Required keys gathered dynamically based on configured model providers
- Set appropriate `<PROVIDER>_API_KEY` environment variable
- Check with `env | grep API_KEY` to verify keys are set
- For MCP tools, check `gateway.RequiredEnvVars()` output for additional secrets

**Tool Execution Failures:**
- Check tool permissions and paths (especially for `shell` and `filesystem` tools)
- For MCP tools, verify command exists and is executable
- Check MCP server logs (stdio stderr captured in debug logs)
- For remote MCP, verify URL accessibility and authentication

**Agent Not Responding:**
- Check max iterations setting - may have hit limit
- Review debug logs for context cancellation or errors
- Verify model API is accessible (check API key and network)
- For DMR provider, ensure Docker Model Runner is enabled and model is pulled

**Performance Issues:**
- Review token usage with `/usage` command during session
- Consider reducing `max_tokens` in model configuration
- Check if MCP tools are slow (show in debug logs)
- For DMR, consider enabling speculative decoding for faster inference

### Debugging Tips

**Use the golang_developer agent:**
```bash
cd /path/to/cagent
./bin/cagent run golang_developer.yaml
# Ask questions about the codebase or request fixes/features
```

**Trace execution flow:**
1. Enable debug mode: `--debug`
2. Look for key log patterns:
   - `"Starting runtime stream"` - Beginning of agent execution
   - `"Tool call"` - Tool being executed
   - `"Tool call result"` - Tool execution completed
   - `"Stream stopped"` - Agent finished

**Test with minimal config:**
```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: "Minimal test agent"
    instruction: "You are a helpful assistant."
```

**Verify build artifacts:**
```bash
task build  # Should create ./bin/cagent
./bin/cagent version  # Should show version info
./bin/cagent --help  # Should list all commands
```

## CI/CD Pipeline

### GitHub Actions Workflow (`.github/workflows/ci.yml`)

**Jobs:**
1. **Lint** - Runs `golangci-lint`
2. **Test** - Runs `task test` (clears API keys for deterministic tests)
3. **License Check** - Validates dependencies use allowed licenses (Apache-2.0, MIT, BSD-3/2-Clause)
4. **Build** - Compiles binary with `task build`
5. **Build Image** - Builds and pushes Docker image for multiple platforms (linux/amd64, linux/arm64)

**Triggers:**
- Push to `main` branch
- Pull requests to `main`
- Tags starting with `v*`
- Manual workflow dispatch

**Build Configuration:**
- Go version: 1.25.5
- Platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64
- Binary name: `cagent` (or `cagent.exe` on Windows)
- Version injection: Uses git tag and commit SHA via ldflags

**Image Publishing:**
- Registry: Docker Hub (`docker/cagent`)
- Tags: semver, edge, PR refs
- Features: SBOM and provenance enabled

### Building Locally

```bash
# Standard build
task build

# Cross-platform builds (requires Docker Buildx)
task build-local   # Current platform only
task cross         # All platforms

# Docker image
task build-image   # Build image
task push-image    # Build and push multi-platform
```

## Important Gotchas and Non-Obvious Patterns

### Configuration Gotchas

1. **Model references are case-sensitive**: `openai/gpt-4o` ≠ `openai/GPT-4o`

2. **Inline vs defined models**: Both work but have different validation
   ```yaml
   # Inline - validated at runtime
   agents:
     root:
       model: openai/gpt-4o

   # Defined - validated at config load
   models:
     my_model:
       provider: openai
       model: gpt-4o
   agents:
     root:
       model: my_model
   ```

3. **Environment variables not in configs**: As of v2, `env` fields removed from agent configs
   - API keys read directly from environment
   - MCP tool env vars specified in toolset config only

4. **transfer_task auto-approved**: Unlike other tools, `transfer_task` always executes without confirmation
   - This allows seamless delegation between agents
   - Other tools respect `sess.ToolsApproved` setting

5. **Toolset order matters for MCP**: First matching tool name wins if multiple toolsets provide same tool

### Code Patterns to Follow

1. **Never use `context.Background()` in functions**: Always accept `context.Context` as first parameter
   - Exception: `main()` function can create root context

2. **Session messages are immutable**: Once added to session, messages aren't modified
   - Create new messages instead of modifying existing ones

3. **Streaming requires buffered channels**: Event channels use capacity 128
   - Prevents blocking when consumer is slow
   - Producer always closes channel when done

4. **Tool results must be serializable**: Tool outputs converted to JSON
   - Complex types need custom marshaling
   - Consider using structured output formats

5. **Agent tools cached during iteration**: Tool list built once per `RunStream()` call
   - Changes to toolsets don't take effect mid-stream
   - Restart agent to pick up tool changes

### Testing Gotchas

1. **VCR cassettes include timestamps**: May need regeneration if assertions check time-sensitive data

2. **Golden files must match exactly**: Including whitespace and line endings
   - Use `golden.Update()` to regenerate when intentionally changing output

3. **Parallel tests share nothing**: Each test gets isolated context, tempdir, and env
   - Don't rely on test execution order

4. **Mock expectations are strict**: `AssertExpectations(t)` fails if methods not called
   - Use `mock.Anything` for flexible argument matching

5. **HTTP test servers use random ports**: Never hardcode port numbers in tests
   - Use `server.URL` from `httptest.NewServer()`

### MCP Tool Integration

1. **Stdio MCP servers block until process exits**:
   - Start/Stop lifecycle managed by toolset
   - Server must respond to `initialize` before tools available

2. **Remote MCP requires SSE or HTTP**:
   - SSE (Server-Sent Events) for streaming
   - HTTP polling as fallback

3. **Docker MCP refs resolve via gateway**:
   ```yaml
   toolsets:
     - type: mcp
       ref: docker:github-official  # Special handling
   ```
   - Requires Docker Desktop or gateway configuration
   - Auto-discovers required environment variables

4. **Tool name collisions handled by toolset order**:
   - First toolset with matching tool name wins
   - Can use `tools: ["specific_tool"]` to filter

5. **Elicitation is blocking**: When MCP tool needs user input
   - Runtime suspends until user provides data
   - Flows through `elicitationRequestCh` channel

### Runtime Behavior

1. **Max iterations default is 0 (unlimited)**:
   - Set `max_iterations` in agent config to prevent infinite loops
   - DMR provider defaults to 20 for safety

2. **Tool confirmation pauses execution**:
   - Runtime emits `ToolCallConfirmation` event
   - Waits on `resumeChan` for user decision
   - Auto-approved if `sess.ToolsApproved` is true

3. **Context cancellation cascades**:
   - Parent context cancel stops all child agents
   - Use `context.WithoutCancel()` for cleanup operations

4. **Streaming partial responses**:
   - `AgentChoice` events may contain partial text (delta)
   - Only complete response stored in session

5. **Session history limits**:
   - Controlled by `num_history_items` in agent config
   - Older messages dropped to fit context window
   - System message always included

### Performance Considerations

1. **Tool discovery is per-agent per-stream**: Cached during single `RunStream()` call
   - Don't repeatedly call `agent.Tools()` - expensive for MCP toolsets

2. **Event channel buffer size matters**:
   - Undersized buffer blocks runtime
   - Oversized buffer wastes memory
   - 128 is sweet spot for most use cases

3. **DMR models run locally**: Resource-intensive
   - Consider `provider_opts.runtime_flags` for memory/GPU tuning
   - Speculative decoding trades memory for speed

4. **Large context windows = high memory**:
   - 64K tokens can use GBs of RAM depending on model
   - Consider shorter `max_tokens` or history limits

5. **Telemetry adds overhead**: Disable with `TELEMETRY_ENABLED=false` for benchmarking

## Quick Reference: Key Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, signal handling |
| `cmd/root/root.go` | Root command, logging setup, persistent flags |
| `cmd/root/run.go` | `cagent run` command implementation |
| `cmd/root/exec.go` | `cagent exec` command (non-TUI) |
| `pkg/runtime/runtime.go` | Core execution loop, tool handling, streaming |
| `pkg/agent/agent.go` | Agent abstraction, tool discovery |
| `pkg/session/session.go` | Message history management |
| `pkg/config/config.go` | Config loading, versioning, migration |
| `pkg/config/latest/types.go` | Current config schema |
| `pkg/tools/tools.go` | Tool interface definitions |
| `pkg/tools/builtin/` | Built-in tool implementations |
| `pkg/tools/mcp/` | MCP protocol client implementations |
| `pkg/model/provider/` | AI provider integrations |
| `pkg/tui/` | Terminal UI (Bubble Tea) |
| `Taskfile.yml` | Build automation tasks |
| `.golangci.yml` | Linter configuration |
| `cagent-schema.json` | JSON schema for config validation |
