package builtin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewShellTool(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	tool := NewShellTool(nil)

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/bash", tool.handler.shell)

	t.Setenv("SHELL", "")
	tool = NewShellTool(nil)

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/sh", tool.handler.shell, "Should default to /bin/sh when SHELL is not set")
}

func TestShellTool_Tools(t *testing.T) {
	tool := NewShellTool(nil)

	allTools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, allTools, 1)
	for _, tool := range allTools {
		assert.NotNil(t, tool.Handler)
		assert.Equal(t, "shell", tool.Category)
	}
	assert.Equal(t, "shell", allTools[0].Name)
	assert.Contains(t, allTools[0].Description, "Executes the given shell command")

	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"cmd": {
			"description": "The shell command to execute",
			"type": "string"
		},
		"cwd": {
			"description": "The working directory to execute the command in",
			"type": "string"
		},
		"timeout": {
			"description": "Command execution timeout in seconds (default: 30)",
			"type": "integer"
		}
	},
	"additionalProperties": false,
	"required": [
		"cmd",
		"cwd"
	]
}`, string(schema))
}

func TestShellTool_DisplayNames(t *testing.T) {
	tool := NewShellTool(nil)

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestShellTool_HandlerEcho(t *testing.T) {
	// This is a simple test that should work on most systems
	tool := NewShellTool(nil)

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	args := RunShellArgs{
		Cmd: "echo 'hello world'",
		Cwd: "",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	result, err := handler(t.Context(), toolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "hello world")
}

func TestShellTool_HandlerWithCwd(t *testing.T) {
	// This test verifies the cwd parameter works
	tool := NewShellTool(nil)

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	tmpDir := t.TempDir() // Create a temporary directory for testing

	args := RunShellArgs{
		Cmd: "pwd",
		Cwd: tmpDir,
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	result, err := handler(t.Context(), toolCall)

	require.NoError(t, err)
	// The output might contain extra newlines or other characters,
	// so we just check if it contains the temp dir path
	assert.Contains(t, result.Output, tmpDir)
}

func TestShellTool_HandlerError(t *testing.T) {
	// This test verifies error handling
	tool := NewShellTool(nil)

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	args := RunShellArgs{
		Cmd: "command_that_does_not_exist",
		Cwd: "",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: string(argsBytes),
		},
	}

	result, err := handler(t.Context(), toolCall)

	require.NoError(t, err, "Handler should not return an error")
	assert.Contains(t, result.Output, "Error executing command")
}

func TestShellTool_InvalidArguments(t *testing.T) {
	tool := NewShellTool(nil)

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	// Invalid JSON
	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: "{invalid json",
		},
	}

	result, err := handler(t.Context(), toolCall)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestShellTool_StartStop(t *testing.T) {
	tool := NewShellTool(nil)

	err := tool.Start(t.Context())
	require.NoError(t, err)

	err = tool.Stop(t.Context())
	require.NoError(t, err)
}

func TestShellTool_OutputSchema(t *testing.T) {
	tool := NewShellTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestShellTool_ParametersAreObjects(t *testing.T) {
	tool := NewShellTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
