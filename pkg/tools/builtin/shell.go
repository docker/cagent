package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameShell = "shell"

type ShellTool struct {
	tools.ElicitationTool
	handler *shellHandler
}

// Make sure Shell Tool implements the ToolSet Interface
var _ tools.ToolSet = (*ShellTool)(nil)

type shellHandler struct {
	shell           string
	shellArgsPrefix []string
	env             []string
	timeout         time.Duration
}

type RunShellArgs struct {
	Cmd     string `json:"cmd" jsonschema:"The shell command to execute"`
	Cwd     string `json:"cwd" jsonschema:"The working directory to execute the command in"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"Command execution timeout in seconds (default: 30)"`
}

func (h *shellHandler) RunShell(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params RunShellArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Determine effective timeout
	effectiveTimeout := h.timeout
	if params.Timeout > 0 {
		effectiveTimeout = time.Duration(params.Timeout) * time.Second
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, effectiveTimeout)
	defer cancel()

	cmd := exec.Command(h.shell, append(h.shellArgsPrefix, params.Cmd)...)
	cmd.Env = h.env
	if params.Cwd != "" {
		cmd.Dir = params.Cwd
	} else {
		if wd, err := os.Getwd(); err == nil {
			cmd.Dir = wd
		}
	}

	cmd.SysProcAttr = platformSpecificSysProcAttr()

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error starting command: %s", err),
		}, nil
	}

	pg, err := createProcessGroup(cmd.Process)
	if err != nil {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error creating process group: %s", err),
		}, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-timeoutCtx.Done():
		if cmd.Process != nil {
			_ = kill(cmd.Process, pg)
		}
		output := outBuf.String()
		// Check if parent context was cancelled or if it was a timeout
		if ctx.Err() != nil {
			// Parent context was cancelled
			return &tools.ToolCallResult{
				Output: "Command cancelled",
			}, nil
		}
		// Timeout occurred
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Command timed out after %v\nOutput: %s", effectiveTimeout, output),
		}, nil
	case err := <-done:
		output := outBuf.String()

		if err != nil {
			return &tools.ToolCallResult{
				Output: fmt.Sprintf("Error executing command: %s\nOutput: %s", err, output),
			}, nil
		}

		if strings.TrimSpace(output) == "" {
			return &tools.ToolCallResult{
				Output: "<no output>",
			}, nil
		}

		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Output: %s", output),
		}, nil
	}
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
			timeout:         30 * time.Second,
		},
	}
}

func (t *ShellTool) Instructions() string {
	return `# Shell Tool Usage Guide

Execute shell commands in the user's environment with full control over working directories and command parameters.

## Core Concepts

**Execution Context**: Commands run in the user's default shell with access to all environment variables and the current workspace.
On Windows, PowerShell (pwsh/powershell) is used when available; otherwise, cmd.exe is used.
On Unix-like systems, ${SHELL} is used or /bin/sh as fallback.

**Working Directory Management**:
- Default execution location: workspace root
- Override with "cwd" parameter for targeted command execution
- Supports both absolute and relative paths

**Command Isolation**: Each tool call creates a fresh shell session - no state persists between executions.

**Timeout Protection**: Commands have a default 30-second timeout to prevent hanging. For longer operations, specify a custom timeout.

## Parameter Reference

| Parameter | Type   | Required | Description |
|-----------|--------|----------|-------------|
| cmd       | string | Yes      | Shell command to execute |
| cwd       | string | Yes      | Working directory (use "." for current) |
| timeout   | int    | No       | Timeout in seconds (default: 30) |

## Best Practices

### ✅ DO
- Leverage the "cwd" parameter for directory-specific commands
- Quote arguments containing spaces or special characters
- Use pipes and redirections
- Write advanced scripts with heredocs, that replace a lot of simple commands or tool calls
- This tool is great at reading and writing multiple files at once
- Avoid writing shell scripts to the disk. Instead, use heredocs to pipe the script to the SHELL
- Use the timeout parameter for long-running operations (e.g., builds, tests)

## Usage Examples

**Basic command execution:**
{ "cmd": "ls -la", "cwd": "." }

**Long-running command with custom timeout:**
{ "cmd": "npm run build", "cwd": ".", "timeout": 120 }

**Language-specific operations:**
{ "cmd": "go test ./...", "cwd": ".", "timeout": 180 }
{ "cmd": "npm install", "cwd": "frontend" }
{ "cmd": "python -m pytest tests/", "cwd": "backend", "timeout": 90 }

**File operations:**
{ "cmd": "find . -name '*.go' -type f", "cwd": "." }
{ "cmd": "grep -r 'TODO' src/", "cwd": "." }

**Process management:**
{ "cmd": "ps aux | grep node", "cwd": "." }
{ "cmd": "docker ps --format 'table {{.Names}}\t{{.Status}}'", "cwd": "." }

**Complex pipelines:**
{ "cmd": "cat package.json | jq '.dependencies'", "cwd": "frontend" }

**Bash scripts:**
{ "cmd": "cat << 'EOF' | ${SHELL}
echo Hello
EOF" }

## Error Handling

Commands that exit with non-zero status codes will return error information along with any output produced before failure.
Commands that exceed their timeout will be terminated automatically.`
}

func (t *ShellTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         ToolNameShell,
			Category:     "shell",
			Description:  `Executes the given shell command in the user's default shell.`,
			Parameters:   tools.MustSchemaFor[RunShellArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.RunShell,
			Annotations: tools.ToolAnnotations{
				Title: "Run Shell Command",
			},
		},
	}, nil
}

func (t *ShellTool) Start(context.Context) error {
	return nil
}

func (t *ShellTool) Stop(context.Context) error {
	return nil
}
