package builtin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewThinkTool(t *testing.T) {
	tool := NewThinkTool()

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Empty(t, tool.handler.thoughts)
}

func TestThinkTool_Instructions(t *testing.T) {
	tool := NewThinkTool()

	instructions := tool.Instructions()
	assert.Contains(t, instructions, "Using the think tool")
	assert.Contains(t, instructions, "Use the think tool generously")
}

func TestThinkTool_Tools(t *testing.T) {
	tool := NewThinkTool()

	allTools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, allTools, 1)

	// Verify think function
	assert.Equal(t, "think", allTools[0].Name)
	assert.Contains(t, allTools[0].Description, "Use the tool to think about something")
	assert.NotNil(t, allTools[0].Handler)

	// Check parameters
	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"thought": {
			"description": "The thought to think about",
			"type": "string"
		}
	},
	"additionalProperties": false,
	"required": [
		"thought"
	]
}`, string(schema))
}

func TestThinkTool_DisplayNames(t *testing.T) {
	tool := NewThinkTool()

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestThinkTool_Handler(t *testing.T) {
	tool := NewThinkTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	// Create tool call with thought
	args := ThinkArgs{
		Thought: "This is a test thought",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "think",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := handler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "This is a test thought")

	// Add another thought
	args.Thought = "Another thought"
	argsBytes, err = json.Marshal(args)
	require.NoError(t, err)

	toolCall.Function.Arguments = string(argsBytes)

	result, err = handler(t.Context(), toolCall)

	// Verify both thoughts are in output
	require.NoError(t, err)
	assert.Contains(t, result.Output, "This is a test thought")
	assert.Contains(t, result.Output, "Another thought")
}

func TestThinkTool_InvalidArguments(t *testing.T) {
	tool := NewThinkTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 1)

	handler := tls[0].Handler

	// Invalid JSON
	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "think",
			Arguments: "{invalid json",
		},
	}

	result, err := handler(t.Context(), toolCall)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestThinkTool_StartStop(t *testing.T) {
	tool := NewThinkTool()

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop()
	require.NoError(t, err)
}

func TestThinkTool_OutputSchema(t *testing.T) {
	tool := NewThinkTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestThinkTool_ParametersAreObjects(t *testing.T) {
	tool := NewThinkTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
