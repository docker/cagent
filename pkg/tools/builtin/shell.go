package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/tools"
)

type ShellTool struct {
	handler *shellHandler
}

// Make sure Shell Tool implements the ToolSet Interface
var _ tools.ToolSet = (*ShellTool)(nil)

type shellHandler struct {
	shell              string
	backgroundCommands sync.Map
	commandOutputs     sync.Map
}

type shellParams struct {
	Cmd        string `json:"cmd"`
	Cwd        string `json:"cwd"`
	Background bool   `json:"background"`
}

func (h *shellHandler) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params shellParams
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	cmd := exec.CommandContext(ctx, h.shell, "-c", params.Cmd)
	cmd.Env = os.Environ()
	if params.Cwd != "" {
		cmd.Dir = params.Cwd
	} else {
		cmd.Dir = os.Getenv("PWD")
	}

	if params.Background {
		pid, err := h.runInBackground(cmd)
		if err != nil {
			return nil, fmt.Errorf("error running command in background: %w", err)
		}

		return &tools.ToolCallResult{
			Output: "Command started in the background, the command pid is: " + pid,
		}, nil
	} else {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return &tools.ToolCallResult{
				Output: fmt.Sprintf("Error executing command: %s\nOutput: %s", err, string(output)),
			}, nil
		}

		out := string(output)
		if strings.TrimSpace(out) == "" {
			out = "Command completed successfully"
		}
		return &tools.ToolCallResult{
			Output: out,
		}, nil
	}
}

func (h *shellHandler) runInBackground(cmd *exec.Cmd) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("error creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("error creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("error starting command: %w", err)
	}

	pid := strconv.Itoa(cmd.Process.Pid)
	h.backgroundCommands.Store(pid, cmd.Process)
	h.commandOutputs.Store(pid, "")

	go func() {
		defer stdout.Close()
		defer stderr.Close()

		stdoutData := make([]byte, 4096)
		stderrData := make([]byte, 4096)

		for {
			n, err := stdout.Read(stdoutData)
			if n > 0 {
				currentOutput, _ := h.commandOutputs.Load(pid)
				h.commandOutputs.Store(pid, currentOutput.(string)+string(stdoutData[:n]))
			}
			if err != nil {
				break
			}

			n, err = stderr.Read(stderrData)
			if n > 0 {
				currentOutput, _ := h.commandOutputs.Load(pid)
				h.commandOutputs.Store(pid, currentOutput.(string)+string(stderrData[:n]))
			}
			if err != nil {
				break
			}
		}

		if err := cmd.Wait(); err != nil {
			currentOutput, _ := h.commandOutputs.Load(pid)
			h.commandOutputs.Store(pid, currentOutput.(string)+fmt.Sprintf("\nCommand exited with error: %v", err))
		} else {
			currentOutput, _ := h.commandOutputs.Load(pid)
			h.commandOutputs.Store(pid, currentOutput.(string)+"\nCommand completed successfully")
		}
	}()

	return pid, nil
}

func (h *shellHandler) GetCommandOutput(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params struct {
		Pid string `json:"pid"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	output, exists := h.commandOutputs.Load(params.Pid)
	if !exists {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("No output found for PID %s. The command may not have been run in the background or the PID is invalid.", params.Pid),
		}, nil
	}

	outputStr := output.(string)
	if strings.TrimSpace(outputStr) == "" {
		return &tools.ToolCallResult{
			Output: "Command is still running, no output available yet",
		}, nil
	}

	return &tools.ToolCallResult{
		Output: outputStr,
	}, nil
}

func NewShellTool() *ShellTool {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh" // Fallback to /bin/sh if SHELL is not set
	}

	return &ShellTool{
		handler: &shellHandler{
			shell:              shell,
			commandOutputs:     sync.Map{},
			backgroundCommands: sync.Map{},
		},
	}
}

func (t *ShellTool) Instructions() string {
	return `# Shell Tool Usage Guide

Execute shell commands in the user's environment with full control over working directories and command parameters.

## Core Concepts

**Execution Context**: Commands run in the user's default shell (${SHELL}) with access to all environment variables and the current workspace.

**Working Directory Management**:
- Default execution location: workspace root
- Override with "cwd" parameter for targeted command execution
- Supports both absolute and relative paths

**Command Isolation**: Each tool call creates a fresh shell session - no state persists between executions.

## Parameter Reference

| Parameter | Type    | Required | Description |
|-----------|---------|----------|-------------|
| cmd       | string  | Yes      | Shell command to execute |
| cwd       | string  | Yes      | Working directory (use "." for current) |
| background| boolean | No       | Run command in background (default: false) |

## Background Commands

**When to Use Background Execution**:
- Long-running processes (servers, build processes, monitoring)
- Commands that take significant time to complete
- When you need to run multiple commands concurrently
- Processes that you want to continue running while performing other tasks

**How Background Commands Work**:
1. Set "background": true in your shell command
2. Command starts immediately and returns a PID
3. Output is collected in real-time by a background goroutine
4. Use "get_command_output" tool to retrieve collected output

**Retrieving Background Command Output**:
Use the "get_command_output" tool with the PID returned from the background command:
{ "pid": 12345 }

**Background Command Lifecycle**:
- Command output is collected continuously while running
- Both stdout and stderr are captured and combined
- Completion status is appended when command finishes
- Output remains available until the shell tool is restarted

## Best Practices

### ✅ DO
- Use separate tool calls for independent operations
- Leverage the "cwd" parameter for directory-specific commands
- Quote arguments containing spaces or special characters
- Use pipes and redirections within a single command
- Use background mode for long-running processes
- Check background command output periodically

### ❌ AVOID
- Chaining unrelated commands with ";" or "&&"
- Relying on state from previous commands
- Complex multi-line scripts (break into separate calls)
- Using background mode for quick commands
- Forgetting to check output of background commands

## Usage Examples

**Basic command execution:**
{ "cmd": "ls -la", "cwd": "." }

**Language-specific operations:**
{ "cmd": "go test ./...", "cwd": "." }
{ "cmd": "npm install", "cwd": "frontend" }
{ "cmd": "python -m pytest tests/", "cwd": "backend" }

**File operations:**
{ "cmd": "find . -name '*.go' -type f", "cwd": "." }
{ "cmd": "grep -r 'TODO' src/", "cwd": "." }

**Process management:**
{ "cmd": "ps aux | grep node", "cwd": "." }
{ "cmd": "docker ps --format 'table {{.Names}}\t{{.Status}}'", "cwd": "." }

**Complex pipelines:**
{ "cmd": "cat package.json | jq '.dependencies'", "cwd": "frontend" }

**Background command examples:**
{ "cmd": "npm run dev", "cwd": "frontend", "background": true }
{ "cmd": "go build -v ./...", "cwd": ".", "background": true }
{ "cmd": "docker build -t myapp .", "cwd": ".", "background": true }
{ "cmd": "python -m http.server 8000", "cwd": "docs", "background": true }

**Checking background command output:**
# First, start a background command and note the PID
{ "cmd": "sleep 10 && echo 'Done!'", "cwd": ".", "background": true }
# Returns: "Command started in the background, the command pid is: 12345"

# Then check its output using the PID
{ "pid": 12345 }

## Error Handling

Commands that exit with non-zero status codes will return error information along with any output produced before failure. For background commands, errors are captured and included in the output retrieved via get_command_output.`
}

func (t *ShellTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Function: &tools.FunctionDefinition{
				Name:        "shell",
				Description: `Executes the given shell command in the user's default shell.`,
				Parameters: tools.FunctionParamaters{
					Type: "object",
					Properties: map[string]any{
						"cmd": map[string]any{
							"type":        "string",
							"description": "The shell command to execute",
						},
						"cwd": map[string]any{
							"type":        "string",
							"description": "The working directory to execute the command in",
						},
						"background": map[string]any{
							"type":        "boolean",
							"description": "Whether to run the command in the background",
						},
					},
					Required: []string{"cmd", "cwd"},
				},
			},
			Handler: t.handler.CallTool,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "get_command_output",
				Description: "Get the output of a command that was run in the background",
				Annotations: tools.ToolAnnotation{
					ReadOnlyHint: &[]bool{true}[0],
				},
				Parameters: tools.FunctionParamaters{
					Type: "object",
					Properties: map[string]any{
						"pid": map[string]any{
							"type":        "string",
							"description": "The pid of the command to get the output of",
						},
					},
					Required: []string{"pid"},
				},
			},
			Handler: t.handler.GetCommandOutput,
		},
	}, nil
}

func (t *ShellTool) Start(context.Context) error {
	return nil
}

func (t *ShellTool) Stop() error {
	var err error
	t.handler.backgroundCommands.Range(func(key, value any) bool {
		if err = value.(*os.Process).Kill(); err != nil {
			return false
		}
		t.handler.backgroundCommands.Delete(key)
		return true
	})

	return err
}
