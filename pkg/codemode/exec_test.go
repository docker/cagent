package codemode

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestCallTool_StringOutput(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "string_tool",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[string]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			return &tools.ToolCallResult{Output: "hello world"}, nil
		},
	}

	callFunc := callTool(context.Background(), tool)
	result, err := callFunc(map[string]any{})

	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
	assert.IsType(t, "", result) // Should be string type
}

func TestCallTool_ArrayOutput(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "array_tool",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[[]string]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			data := []string{"file1.txt", "file2.txt"}
			jsonData, _ := json.Marshal(data)
			return &tools.ToolCallResult{Output: string(jsonData)}, nil
		},
	}

	callFunc := callTool(context.Background(), tool)
	result, err := callFunc(map[string]any{})

	require.NoError(t, err)
	assert.IsType(t, []any{}, result) // Should be parsed as array
	
	// Convert result to []any and check contents
	resultArray := result.([]any)
	assert.Len(t, resultArray, 2)
	assert.Equal(t, "file1.txt", resultArray[0])
	assert.Equal(t, "file2.txt", resultArray[1])
}

func TestCallTool_ObjectOutput(t *testing.T) {
	type FileInfo struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "object_tool",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[FileInfo]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			data := FileInfo{Name: "test.txt", Size: 1024}
			jsonData, _ := json.Marshal(data)
			return &tools.ToolCallResult{Output: string(jsonData)}, nil
		},
	}

	callFunc := callTool(context.Background(), tool)
	result, err := callFunc(map[string]any{})

	require.NoError(t, err)
	assert.IsType(t, map[string]any{}, result) // Should be parsed as object

	// Convert result to map and check contents
	resultMap := result.(map[string]any)
	assert.Equal(t, "test.txt", resultMap["name"])
	assert.Equal(t, float64(1024), resultMap["size"]) // JSON numbers become float64
}

func TestCallTool_NoSchema(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "no_schema_tool",
			OutputSchema: nil,
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			return &tools.ToolCallResult{Output: "raw output"}, nil
		},
	}

	callFunc := callTool(context.Background(), tool)
	result, err := callFunc(map[string]any{})

	require.NoError(t, err)
	assert.Equal(t, "raw output", result)
	assert.IsType(t, "", result) // Should remain as string
}

func TestCallTool_InvalidJSON(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "invalid_json_tool",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[[]string]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			return &tools.ToolCallResult{Output: "invalid json {"}, nil
		},
	}

	callFunc := callTool(context.Background(), tool)
	result, err := callFunc(map[string]any{})

	require.NoError(t, err)
	// Should fallback to string when JSON parsing fails
	assert.Equal(t, "invalid json {", result)
	assert.IsType(t, "", result)
}