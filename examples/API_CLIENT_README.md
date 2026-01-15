# Cagent API Client Example

This example demonstrates how to interact with the cagent API server programmatically.

## What it does

The `api-client.js` script:
1. Creates a new session via the API
2. Sends a message to the agent asking it to run the "start-task" tool
3. Streams and prints the agent's response in real-time

## Usage

### Step 1: Start the API server

In one terminal, start the cagent API server:

```bash
cagent api examples/nick-bug-report.yaml
```

By default, this starts the server on `http://localhost:8080`.

### Step 2: Run the client script

In another terminal, run the client script:

```bash
node examples/api-client.js
```

Or make it executable and run directly:

```bash
chmod +x examples/api-client.js
./examples/api-client.js
```

## Expected Output

You should see output similar to:

```
Creating session...
Session created: abc123

Running agent...

[Stream Started] Session: abc123, Agent: root
[Tool Call] start-task
Arguments: {}
[Tool Response]
Error: Missing required environment variables

[Stream Stopped] Reason: stop
✓ Agent finished
```

## API Endpoints Used

- `POST /api/sessions` - Creates a new session
- `POST /api/sessions/:id/agent/:agent` - Runs an agent with messages (streaming response via SSE)

## Customization

You can modify the script to:
- Change the message sent to the agent (line 143)
- Use a different agent configuration (change `AGENT_ID`)
- Change the API server URL (change `API_BASE_URL`)
- Add more sophisticated event handling in `handleEvent()`

## Session Options

When creating a session, you can configure:

```javascript
const sessionData = {
  title: 'My Session',           // Session title
  tools_approved: true,           // Auto-approve tool calls (YOLO mode)
  working_dir: '/some/path',      // Working directory
  permissions: {                  // Permission configuration
    allowed_prompts: [...],
  }
};
```

## See Also

- [USAGE.md](../docs/USAGE.md) - Full cagent documentation
- [AGENTS.md](../AGENTS.md) - Agent configuration guide
