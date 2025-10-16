package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"

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
	mu              sync.Mutex
	processes       []*os.Process
}

type RunShellArgs struct {
	Cmd string `json:"cmd" jsonschema:"The shell command to execute"`
	Cwd string `json:"cwd" jsonschema:"The working directory to execute the command in"`
}

func (h *shellHandler) RunShell(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params RunShellArgs
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

	// Set up process group for proper cleanup
	// On Unix: create new process group so we can kill the entire tree
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}
	}
	// Note: On Windows, we would set CreationFlags, but that requires
	// platform-specific code in a _windows.go file

	// Capture output using buffers
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Start the command so we can track it
	if err := cmd.Start(); err != nil {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error starting command: %s", err),
		}, nil
	}

	// Track the process for cleanup
	h.mu.Lock()
	h.processes = append(h.processes, cmd.Process)
	h.mu.Unlock()

	// Remove from tracking once complete
	defer func() {
		h.mu.Lock()
		for i, p := range h.processes {
			if p != nil && p.Pid == cmd.Process.Pid {
				h.processes = append(h.processes[:i], h.processes[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
	}()

	// Wait for the command to complete and get the result
	err := cmd.Wait()

	// Combine stdout and stderr
	output := outBuf.String() + errBuf.String()

	if err != nil {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error executing command: %s\nOutput: %s", err, output),
		}, nil
	}

	if len(output) == 0 {
		return &tools.ToolCallResult{
			Output: "<no output>",
		}, nil
	}

	return &tools.ToolCallResult{
		Output: output,
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

**Execution Context**: Commands run in the user's default shell with access to all environment variables and the current workspace.
On Windows, PowerShell (pwsh/powershell) is used when available; otherwise, cmd.exe is used.
On Unix-like systems, ${SHELL} is used or /bin/sh as fallback.

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
- Leverage the "cwd" parameter for directory-specific commands
- Quote arguments containing spaces or special characters
- Use pipes and redirections
- Write advanced scripts with heredocs, that replace a lot of simple commands or tool calls
- This tool is great at reading and writing multiple files at once
- Avoid writing shell scripts to the disk. Instead, use heredocs to pipe the script to the SHELL

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

**Bash scripts:**
{ "cmd": "cat << 'EOF' | ${SHELL}
echo Hello
EOF" }

## Error Handling

Commands that exit with non-zero status codes will return error information along with any output produced before failure.`
}

func (t *ShellTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         "shell",
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

func (t *ShellTool) Stop() error {
	t.handler.mu.Lock()
	defer t.handler.mu.Unlock()

	// Kill all tracked processes
	for _, proc := range t.handler.processes {
		if proc == nil {
			continue
		}

		// On Unix: kill the entire process group
		// On Windows: terminate the process
		if runtime.GOOS == "windows" {
			// On Windows, we kill the process directly
			_ = proc.Kill()
		} else {
			// On Unix, kill the process group (negative PID kills the group)
			// We use SIGTERM first for graceful shutdown
			_ = syscall.Kill(-proc.Pid, syscall.SIGTERM)

			// Note: We could add a timeout and send SIGKILL if processes don't stop,
			// but for now we'll just send SIGTERM
		}
	}

	// Clear the processes list
	t.handler.processes = nil

	return nil
}
