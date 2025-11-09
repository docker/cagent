package codemode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestCodeModeTool_Tools(t *testing.T) {
	tool := &codeModeTool{}

	toolSet, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolSet, 1)

	fetchTool := toolSet[0]
	assert.Equal(t, "run_tools_with_javascript", fetchTool.Name)
	assert.Equal(t, "code mode", fetchTool.Category)
	assert.NotNil(t, fetchTool.Handler)

	inputSchema, err := json.Marshal(fetchTool.Parameters)
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
}`, string(inputSchema))

	outputSchema, err := json.Marshal(fetchTool.OutputSchema)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"required": [
		"value",
		"stdout",
		"stderr"
	],
	"properties": {
		"stderr": {
			"type": "string",
			"description": "The standard error of the console"
		},
		"stdout": {
			"type": "string",
			"description": "The standard output of the console"
		},
		"value": {
			"type": "string",
			"description": "The value returned by the script"
		}
	},
	"additionalProperties": false
}`, string(outputSchema))
}

func TestCodeModeTool_Instructions(t *testing.T) {
	tool := &codeModeTool{}

	instructions := tool.Instructions()

	assert.Empty(t, instructions)
}

func TestCodeModeTool_StartStop(t *testing.T) {
	inner := &testToolSet{}

	tool := Wrap(inner)

	assert.Equal(t, 0, inner.start)
	assert.Equal(t, 0, inner.stop)

	err := tool.Start(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, inner.start)
	assert.Equal(t, 0, inner.stop)

	err = tool.Stop(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, inner.start)
	assert.Equal(t, 1, inner.stop)
}

func TestCodeModeTool_CallHello(t *testing.T) {
	tool := Wrap(&testToolSet{
		tools: []tools.Tool{{
			Name: "hello_world",
			Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
				return &tools.ToolCallResult{
					Output: "Hello, World!",
				}, nil
			},
		}},
	})

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 1)

	result, err := allTools[0].Handler(t.Context(), tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: `{"script":"return hello_world();"}`,
		},
	})
	require.NoError(t, err)

	var scriptResult ScriptResult
	err = json.Unmarshal([]byte(result.Output), &scriptResult)
	require.NoError(t, err)

	require.Equal(t, "Hello, World!", scriptResult.Value)
	require.Empty(t, scriptResult.StdErr)
	require.Empty(t, scriptResult.StdOut)
}

func TestCodeModeTool_CallEcho(t *testing.T) {
	type EchoArgs struct {
		Message string `json:"message" jsonschema:"Message to echo"`
	}

	tool := Wrap(&testToolSet{
		tools: []tools.Tool{{
			Name: "echo",
			Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
				return &tools.ToolCallResult{
					Output: "ECHO",
				}, nil
			},
			Parameters: tools.MustSchemaFor[EchoArgs](),
		}},
	})

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 1)

	result, err := allTools[0].Handler(t.Context(), tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: `{"script":"return echo({'message':'ECHO'});"}`,
		},
	})
	require.NoError(t, err)

	var scriptResult ScriptResult
	err = json.Unmarshal([]byte(result.Output), &scriptResult)
	require.NoError(t, err)

	require.Equal(t, "ECHO", scriptResult.Value)
	require.Empty(t, scriptResult.StdErr)
	require.Empty(t, scriptResult.StdOut)
}

type testToolSet struct {
	tools.ElicitationTool

	tools []tools.Tool
	start int
	stop  int
}

func (t *testToolSet) Tools(ctx context.Context) ([]tools.Tool, error) {
	return t.tools, nil
}

func (t *testToolSet) Instructions() string {
	return ""
}

func (t *testToolSet) Start(context.Context) error {
	t.start++
	return nil
}

func (t *testToolSet) Stop(context.Context) error {
	t.stop++
	return nil
}
