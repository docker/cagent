package codemode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeModeTool_Tools(t *testing.T) {
	tool := &codeModeTool{}

	toolSet, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolSet, 1)

	fetchTool := toolSet[0]
	assert.Equal(t, "run_tools_with_javascript", fetchTool.Name)
	assert.NotNil(t, fetchTool.Handler)

	schema, err := json.Marshal(fetchTool.Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"required": [
		"script"
	],
	"properties": {
		"script": {
			"type": "string",
			"description": "Script to execute"
		}
	},
	"additionalProperties": false
}`, string(schema))
}

func TestCodeModeTool_Instructions(t *testing.T) {
	tool := &codeModeTool{}

	instructions := tool.Instructions()

	assert.Empty(t, instructions)
}

func TestCodeModeTool_StartStop(t *testing.T) {
	tool := &codeModeTool{}

	err := tool.Start(t.Context())
	require.NoError(t, err)

	err = tool.Stop()
	require.NoError(t, err)
}
