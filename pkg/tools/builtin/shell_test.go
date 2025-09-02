package builtin

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewShellTool(t *testing.T) {
	// Save original env
	originalShell := os.Getenv("SHELL")
	defer os.Setenv("SHELL", originalShell)

	// Test with SHELL env var set
	os.Setenv("SHELL", "/bin/bash")
	tool := NewShellTool()

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/bash", tool.handler.shell)

	// Test with no SHELL env var
	os.Setenv("SHELL", "")
	tool = NewShellTool()

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/sh", tool.handler.shell, "Should default to /bin/sh when SHELL is not set")
}

func TestShellTool_Tools(t *testing.T) {
	tool := NewShellTool()

	tools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, tools, 2)

	// Verify bash function
	assert.Equal(t, "shell", tools[0].Function.Name)
	assert.Contains(t, tools[0].Function.Description, "Executes the given shell command")

	// Check parameters
	props := tools[0].Function.Parameters.Properties
	assert.Contains(t, props, "cmd")
	assert.Contains(t, props, "cwd")

	// Check required fields
	assert.Contains(t, tools[0].Function.Parameters.Required, "cmd")
	assert.Contains(t, tools[0].Function.Parameters.Required, "cwd")

	// Verify handler is provided
	assert.NotNil(t, tools[0].Handler)
}

func TestShellTool_DisplayNames(t *testing.T) {
	tool := NewShellTool()

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEqual(t, tool.Function.Name, tools.DisplayName(tool.Function.Name))
	}
}

func TestShellTool_HandlerEcho(t *testing.T) {
	// This is a simple test that should work on most systems
	tool := NewShellTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)

	handler := tls[0].Handler

	// Create tool call for a simple echo command
	args := struct {
		Cmd string `json:"cmd"`
		Cwd string `json:"cwd"`
	}{
		Cmd: "echo 'hello world'",
		Cwd: "",
	}
	argsBytes, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := handler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "hello world")
}

func TestShellTool_HandlerWithCwd(t *testing.T) {
	// This test verifies the cwd parameter works
	tool := NewShellTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)

	handler := tls[0].Handler

	// Create tool call for pwd command with specific cwd
	tmpDir := t.TempDir() // Create a temporary directory for testing

	args := struct {
		Cmd string `json:"cmd"`
		Cwd string `json:"cwd"`
	}{
		Cmd: "pwd",
		Cwd: tmpDir,
	}
	argsBytes, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := handler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	// The output might contain extra newlines or other characters,
	// so we just check if it contains the temp dir path
	assert.True(t, strings.Contains(result.Output, tmpDir),
		"Expected output to contain the temp dir path: %s, but got: %s", tmpDir, result.Output)
}

func TestShellTool_HandlerError(t *testing.T) {
	// This test verifies error handling
	tool := NewShellTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)

	handler := tls[0].Handler

	// Create tool call for a command that should fail
	args := struct {
		Cmd string `json:"cmd"`
		Cwd string `json:"cwd"`
	}{
		Cmd: "command_that_does_not_exist",
		Cwd: "",
	}
	argsBytes, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := handler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err, "Handler should not return an error")
	assert.Contains(t, result.Output, "Error executing command")
}

func TestShellTool_InvalidArguments(t *testing.T) {
	tool := NewShellTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)

	handler := tls[0].Handler

	// Invalid JSON
	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: "{invalid json",
		},
	}

	result, err := handler(t.Context(), toolCall)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestShellTool_StartStop(t *testing.T) {
	tool := NewShellTool()

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop()
	require.NoError(t, err)
}
