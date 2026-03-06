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

func (m *mockMCPClient) SetToolListChangedHandler(func()) {}

func (m *mockMCPClient) SetPromptListChangedHandler(func()) {}

func (m *mockMCPClient) Wait() error { return nil }

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

func TestProcessMCPContent_WithImages(t *testing.T) {
	t.Parallel()

	t.Run("text only", func(t *testing.T) {
		t.Parallel()
		result := processMCPContent(&mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "hello"}},
		})
		assert.Equal(t, "hello", result.Output)
		assert.Empty(t, result.Images)
		assert.False(t, result.IsError)
	})

	t.Run("image only", func(t *testing.T) {
		t.Parallel()
		// mcp.ImageContent.Data holds raw bytes (SDK decodes base64 from wire)
		rawImageData := []byte("imagedata")
		result := processMCPContent(&mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.ImageContent{
					Data:     rawImageData,
					MIMEType: "image/png",
				},
			},
		})
		assert.Equal(t, "no output", result.Output) // no text content, so default
		require.Len(t, result.Images, 1)
		assert.Equal(t, "image/png", result.Images[0].MimeType)
		// Output should be base64-encoded
		assert.Equal(t, "aW1hZ2VkYXRh", result.Images[0].Data)
	})

	t.Run("text and image", func(t *testing.T) {
		t.Parallel()
		result := processMCPContent(&mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Here is the screenshot"},
				&mcp.ImageContent{
					Data:     []byte("screenshot"),
					MIMEType: "image/jpeg",
				},
			},
		})
		assert.Equal(t, "Here is the screenshot", result.Output)
		require.Len(t, result.Images, 1)
		assert.Equal(t, "image/jpeg", result.Images[0].MimeType)
		assert.Equal(t, "c2NyZWVuc2hvdA==", result.Images[0].Data)
	})

	t.Run("error with image", func(t *testing.T) {
		t.Parallel()
		result := processMCPContent(&mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "error occurred"},
				&mcp.ImageContent{
					Data:     []byte("error"),
					MIMEType: "image/png",
				},
			},
		})
		assert.True(t, result.IsError)
		assert.Equal(t, "error occurred", result.Output)
		require.Len(t, result.Images, 1)
		assert.Equal(t, "ZXJyb3I=", result.Images[0].Data)
	})
}
