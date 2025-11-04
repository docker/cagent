# Cagent Chat Client

A command-line interactive chat client for the cagent API deployed on Google Cloud Run.

## Features

- 🎭 **Multiple Agents**: Chat with various specialized agents (basic, pirate, haiku, code experts, etc.)
- 🌊 **Streaming Responses**: Responses are streamed and displayed as complete messages for a smooth chat experience
- 🎨 **Colored Output**: Beautiful terminal colors for better readability (can be disabled)
- 📝 **Chat History**: View your conversation history during the session
- 💰 **Token Usage Tracking**: See token consumption and costs for each interaction
- 📊 **Session Summary**: Get statistics at the end of your chat session

## Installation

The script requires Python 3 with the `requests` library:

```bash
pip install requests
```

## Usage

### Basic Usage

```bash
# Chat with an agent (with or without .yaml extension)
./cagent_chat.py basic_agent.yaml
./cagent_chat.py haiku
./cagent_chat.py pirate

# List all available agents
./cagent_chat.py --list
./cagent_chat.py  # No arguments also shows the list

# Disable colored output
./cagent_chat.py basic_agent --no-color
```

### During Chat

While chatting, you can use these commands:

- `quit`, `exit`, or `q` - End the chat session
- `clear` - Clear the screen
- `history` - Show the complete conversation history
- `Ctrl+C` - Exit immediately

## Available Agents

Run `./cagent_chat.py --list` to see the current list of available agents:

- **basic_agent.yaml** - A helpful AI assistant for general questions
- **haiku.yaml** - Writes creative haikus
- **pirate.yaml** - Responds like a pirate (Arrr!)
- **code.yaml** - Expert code analysis and development assistant
- **pythonist.yaml** - Expert Python developer
- **gopher.yaml** - Expert Golang developer
- **writer.yaml** - Writes creative stories
- **dev-team.yaml** - Product Manager for development teams
- And more...

## Example Session

```bash
$ ./cagent_chat.py haiku

Checking agent availability...
Starting chat session with haiku...
✓ Session created: a3d82cf1-d629-4f7f-9e79-34ed0b3b1197

Chat with haiku
Type 'quit', 'exit', or press Ctrl+C to end the chat
Type 'clear' to clear the screen
Type 'history' to see message history
------------------------------------------------------------

You: Write a haiku about cloud computing

haiku: Servers drift like mist,
Data hums in borrowed skies—
Requests fall as rain.

[Tokens: in=47, out=145, cost=$0.000302]

You: exit

Chat session ended

Session Summary:
  • Messages exchanged: 2
  • Input tokens: 47
  • Output tokens: 145
  • Title: Cloud Computing Haiku
```

## Non-Interactive Mode

You can also use the client in scripts with input redirection:

```bash
# Single question
echo "What is Python?" | ./cagent_chat.py basic_agent

# Multiple questions
cat << EOF | ./cagent_chat.py code
How do I reverse a string in Python?
exit
EOF
```

## API Endpoint

The client connects to the cagent API at:
`https://cagent-api-950783879036.us-central1.run.app`

## Troubleshooting

1. **Connection errors**: Check your internet connection
2. **Agent not found**: Use `--list` to see available agents
3. **No response**: The API might be starting up, try again in a few seconds
4. **Color issues**: Use `--no-color` flag if your terminal doesn't support ANSI colors

## Technical Details

- Uses Server-Sent Events (SSE) for streaming responses
- Maintains session state across messages
- Automatically handles token-by-token streaming
- Displays complete messages instead of partial tokens for better readability