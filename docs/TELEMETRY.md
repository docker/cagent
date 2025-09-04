# Telemetry for cagent

The telemetry system in `cagent` collects **anonymous usage data** to help improve the tool. All events are processed **synchronously** when recorded. Telemetry can be disabled at any time.

On first startup, `cagent` displays a notice about telemetry collection and how to disable it, so users are always informed.

---

## Quick Start

### Disable telemetry

```bash
# Environment variable
TELEMETRY_ENABLED=false cagent run agent.yaml

# The application defaults to enabled when no environment variable is set
```

### Enable debug mode (see events locally)

```bash
# Use the --debug flag to see telemetry events printed locally
cagent run agent.yaml --debug
```

---

## What's Collected ✅

- Command names and success/failure status
- Agent names and model types
- Tool names and whether calls succeed or fail
- Token counts (input/output totals) and estimated costs
- Session metadata (durations, error counts)

## What's NOT Collected ❌

- User input or prompts
- Agent responses or generated content
- File contents
- API keys or credentials
- Personally identifying information

---

## For Developers

Telemetry is automatically wrapped around all commands. If you need to record additional events, use the context-based approach:

```go
// Recommended: Use context-based telemetry (clean, testable)
if telemetryClient := telemetry.FromContext(ctx); telemetryClient != nil {
    telemetryClient.RecordToolCall(ctx, "filesystem", "session-id", "agentName", time.Millisecond*500, nil)
    telemetryClient.RecordTokenUsage(ctx, "gpt-4", 100, 50, 0.01)
}

// Or use direct telemetry calls:
telemetry.TrackCommand("run", args)
```

### Event Types

The system uses structured, type-safe events:

- **Command events**: Track CLI command execution with success status
- **Tool events**: Track agent tool calls with timing and error information
- **Token events**: Track LLM token usage by model, session, and cost
- **Session events**: Track agent session lifecycle with separate start/end events and aggregate metrics

Events are processed synchronously when `Track()` is called, sending HTTP requests immediately.

### Configuration

The system respects these environment variables:

- `TELEMETRY_ENABLED=false` - Disable telemetry collection (default: enabled)
- `TELEMETRY_ENDPOINT=<url>` - Remote telemetry endpoint (optional)
- `TELEMETRY_API_KEY=<key>` - API key for remote telemetry (optional)
- `TELEMETRY_HEADER=<header>` - Authorization header for remote telemetry (optional)

The `--debug` flag enables local event logging without affecting HTTP transmission.
