package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"

	"github.com/docker/cagent/pkg/tools"
)

type ShellTool struct {
	elicitationTool
	handler *shellHandler
}

// Make sure Shell Tool implements the ToolSet Interface
var _ tools.ToolSet = (*ShellTool)(nil)

type shellHandler struct {
	shell           string
	shellArgsPrefix []string
	env             []string
}

func (h *shellHandler) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params struct {
		Cmd string `json:"cmd"`
		Cwd string `json:"cwd"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	cmd := exec.CommandContext(ctx, h.shell, append(h.shellArgsPrefix, params.Cmd)...)
	cmd.Env = h.env
	if params.Cwd != "" {
		cmd.Dir = params.Cwd
	} else {
		// Use the current working directory; avoid PWD on Windows (may be MSYS-style like /c/...)
		if wd, err := os.Getwd(); err == nil {
			cmd.Dir = wd
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error executing command: %s\nOutput: %s", err, string(output)),
		}, nil
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Output: %s", string(output)),
	}, nil
}

func NewShellTool(env []string) *ShellTool {
	var shell string
	var argsPrefix []string

	if runtime.GOOS == "windows" {
		// Prefer PowerShell (pwsh or Windows PowerShell) when available, otherwise fall back to cmd.exe
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			shell = path
			argsPrefix = []string{"-NoProfile", "-NonInteractive", "-Command"}
		} else if path, err := exec.LookPath("powershell.exe"); err == nil {
			shell = path
			argsPrefix = []string{"-NoProfile", "-NonInteractive", "-Command"}
		} else {
			// Use ComSpec if available, otherwise default to cmd.exe
			if comspec := os.Getenv("ComSpec"); comspec != "" {
				shell = comspec
			} else {
				shell = "cmd.exe"
			}
			argsPrefix = []string{"/C"}
		}
	} else {
		// Unix-like: use SHELL or default to /bin/sh
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		argsPrefix = []string{"-c"}
	}

	return &ShellTool{
		handler: &shellHandler{
			shell:           shell,
			shellArgsPrefix: argsPrefix,
			env:             env,
		},
	}
}

func (t *ShellTool) Instructions() string {
	return `# Shell Tool Usage Guide

Execute shell commands in the user's environment with full control over working directories and command parameters.

## Core Concepts

**Execution Context**: Commands run in the user's default shell with access to all environment variables and the current workspace. On Windows, PowerShell (pwsh/powershell) is used when available; otherwise, cmd.exe is used. On Unix-like systems, ${SHELL} is used or /bin/sh as fallback.

**Working Directory Management**:
- Default execution location: workspace root
- Override with "cwd" parameter for targeted command execution
- Supports both absolute and relative paths

**Command Isolation**: Each tool call creates a fresh shell session - no state persists between executions.

## Parameter Reference

| Parameter | Type   | Required | Description |
|-----------|--------|----------|-------------|
| cmd       | string | Yes      | Shell command to execute |
| cwd       | string | Yes      | Working directory (use "." for current) |

## Best Practices

### ✅ DO
- Use separate tool calls for independent operations
- Leverage the "cwd" parameter for directory-specific commands
- Quote arguments containing spaces or special characters
- Use pipes and redirections within a single command

### ❌ AVOID
- Chaining unrelated commands with ";" or "&&"
- Relying on state from previous commands
- Complex multi-line scripts (break into separate calls)

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

## Error Handling

Commands that exit with non-zero status codes will return error information along with any output produced before failure.`
}

func (t *ShellTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Function: tools.FunctionDefinition{
				Name:        "shell",
				Description: `Executes the given shell command in the user's default shell.`,
				Annotations: tools.ToolAnnotations{
					Title: "Run Shell Command",
				},
				Parameters: tools.FunctionParameters{
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
					},
					Required: []string{"cmd", "cwd"},
				},
				OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[string]()),
			},
			Handler: t.handler.CallTool,
		},
	}, nil
}

func (t *ShellTool) Start(context.Context) error {
	return nil
}

func (t *ShellTool) Stop() error {
	return nil
}
