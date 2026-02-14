package mcp

import (
	"context"
	"iter"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

// mockMCPClient is a test double for the mcpClient interface.
type mockMCPClient struct {
	callToolFn func(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

func (m *mockMCPClient) Initialize(context.Context, *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}

func (m *mockMCPClient) ListTools(context.Context, *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error] {
	return func(func(*mcp.Tool, error) bool) {}
}

func (m *mockMCPClient) CallTool(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return m.callToolFn(ctx, request)
}

func (m *mockMCPClient) ListPrompts(context.Context, *mcp.ListPromptsParams) iter.Seq2[*mcp.Prompt, error] {
	return func(func(*mcp.Prompt, error) bool) {}
}

func (m *mockMCPClient) GetPrompt(context.Context, *mcp.GetPromptParams) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{}, nil
}

func (m *mockMCPClient) SetElicitationHandler(tools.ElicitationHandler) {}

func (m *mockMCPClient) SetOAuthSuccessHandler(func()) {}

func (m *mockMCPClient) SetManagedOAuth(bool) {}

func (m *mockMCPClient) Close(context.Context) error { return nil }

func TestCallToolStripsNullArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		arguments    string
		expectedArgs map[string]any
	}{
		{
			name:         "all null values are stripped",
			arguments:    `{"dir": null, "pattern": null}`,
			expectedArgs: map[string]any{},
		},
		{
			name:         "only null values are stripped",
			arguments:    `{"dir": ".", "pattern": null, "extra": "value"}`,
			expectedArgs: map[string]any{"dir": ".", "extra": "value"},
		},
		{
			name:         "empty arguments stay empty",
			arguments:    `{}`,
			expectedArgs: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedArgs map[string]any

			ts := &Toolset{
				started: true,
				mcpClient: &mockMCPClient{
					callToolFn: func(_ context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
						if m, ok := request.Arguments.(map[string]any); ok {
							capturedArgs = m
						}
						return &mcp.CallToolResult{
							Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
						}, nil
					},
				},
			}

			result, err := ts.callTool(t.Context(), tools.ToolCall{
				Function: tools.FunctionCall{
					Name:      "test_tool",
					Arguments: tt.arguments,
				},
			})

			require.NoError(t, err)
			assert.Equal(t, "ok", result.Output)
			assert.Equal(t, tt.expectedArgs, capturedArgs)
		})
	}
}
