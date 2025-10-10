package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

// TestCodeModeIntegration demonstrates how code mode now works with different output schemas
func TestCodeModeIntegration(t *testing.T) {
	// Create a mock tool that returns JSON array
	arrayTool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "get_files",
			Description:  "Get list of files",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[[]string]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			files := []string{"file1.txt", "file2.txt", "file3.txt"}
			jsonData, _ := json.Marshal(files)
			return &tools.ToolCallResult{Output: string(jsonData)}, nil
		},
	}

	// Create a mock tool that returns JSON object
	objectTool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:         "get_stats",
			Description:  "Get file statistics",
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[map[string]any]()),
		},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			stats := map[string]any{"count": 3, "total_size": 1024}
			jsonData, _ := json.Marshal(stats)
			return &tools.ToolCallResult{Output: string(jsonData)}, nil
		},
	}

	// Create a simple toolset that includes our mock tools
	mockToolset := &mockToolSet{tools: []tools.Tool{arrayTool, objectTool}}

	// Create code mode wrapper
	codeMode := Wrap([]tools.ToolSet{mockToolset})

	// Get the javascript tool
	jsTool, err := codeMode.Tools(context.Background())
	require.NoError(t, err)
	require.Len(t, jsTool, 1)

	// Check that the documentation includes the correct return types
	description := jsTool[0].Function.Description
	assert.Contains(t, description, "get_files(): string[]", "Array tool should show string[] return type")
	assert.Contains(t, description, "get_stats(): object", "Object tool should show object return type")

	// Test calling the JavaScript tool with a script that uses both tools
	script := `
		const files = get_files();
		const stats = get_stats();
		
		return "Found " + files.length + " files with total count: " + stats.count;
	`

	result, err := jsTool[0].Handler(context.Background(), tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "run_tools_with_javascript",
			Arguments: fmt.Sprintf(`{"script": %q}`, script),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Found 3 files with total count: 3", result.Output)
}

// TestCodeModeWithBuiltinTools tests with actual builtin tools
func TestCodeModeWithBuiltinTools(t *testing.T) {
	// Create a think tool (which has string output)
	thinkTool := builtin.NewThinkTool()

	// Create code mode with think tool
	codeMode := Wrap([]tools.ToolSet{thinkTool})

	// Get the javascript tool
	jsTool, err := codeMode.Tools(context.Background())
	require.NoError(t, err)
	require.Len(t, jsTool, 1)

	// Check that think tool shows string return type
	description := jsTool[0].Function.Description
	assert.Contains(t, description, "think(args: ArgsObject): string")

	// Test using the think tool in JavaScript
	script := `
		const thought = think({thought: "Testing code mode"});
		return "Thought result: " + thought;
	`

	result, err := jsTool[0].Handler(context.Background(), tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "run_tools_with_javascript",
			Arguments: fmt.Sprintf(`{"script": %q}`, script),
		},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Thought result: Thoughts:")
	assert.Contains(t, result.Output, "Testing code mode")
}

// Mock toolset for testing
type mockToolSet struct {
	tools []tools.Tool
}

func (m *mockToolSet) Tools(ctx context.Context) ([]tools.Tool, error) {
	return m.tools, nil
}

func (m *mockToolSet) Instructions() string {
	return ""
}

func (m *mockToolSet) Start(ctx context.Context) error {
	return nil
}

func (m *mockToolSet) Stop() error {
	return nil
}

func (m *mockToolSet) SetElicitationHandler(handler tools.ElicitationHandler) {
}

func (m *mockToolSet) SetOAuthSuccessHandler(handler func()) {
}