package bedrock

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/tools"
)

func TestConvertMessages_UserText(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{{
		Role:    chat.MessageRoleUser,
		Content: "Hello, world!",
	}}

	bedrockMsgs, system := convertMessages(msgs)

	require.Len(t, bedrockMsgs, 1)
	assert.Empty(t, system)
	assert.Equal(t, types.ConversationRoleUser, bedrockMsgs[0].Role)
	require.Len(t, bedrockMsgs[0].Content, 1)

	textBlock, ok := bedrockMsgs[0].Content[0].(*types.ContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "Hello, world!", textBlock.Value)
}

func TestConvertMessages_SystemExtraction(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "Be helpful"},
		{Role: chat.MessageRoleUser, Content: "Hi"},
	}

	bedrockMsgs, system := convertMessages(msgs)

	require.Len(t, bedrockMsgs, 1) // Only user message
	require.Len(t, system, 1)      // System extracted

	systemBlock, ok := system[0].(*types.SystemContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "Be helpful", systemBlock.Value)
}

func TestConvertMessages_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{{
		Role: chat.MessageRoleAssistant,
		ToolCalls: []tools.ToolCall{{
			ID:   "tool-1",
			Type: "function",
			Function: tools.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"location":"NYC"}`,
			},
		}},
	}}

	bedrockMsgs, _ := convertMessages(msgs)

	require.Len(t, bedrockMsgs, 1)
	require.Len(t, bedrockMsgs[0].Content, 1)

	// Verify tool use block
	toolUse, ok := bedrockMsgs[0].Content[0].(*types.ContentBlockMemberToolUse)
	require.True(t, ok)
	assert.Equal(t, "tool-1", *toolUse.Value.ToolUseId)
	assert.Equal(t, "get_weather", *toolUse.Value.Name)
}

func TestConvertMessages_ToolResult(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{{
		Role:       chat.MessageRoleTool,
		ToolCallID: "tool-1",
		Content:    "Weather is sunny",
	}}

	bedrockMsgs, _ := convertMessages(msgs)

	require.Len(t, bedrockMsgs, 1)
	assert.Equal(t, types.ConversationRoleUser, bedrockMsgs[0].Role)

	// Verify tool result block
	toolResult, ok := bedrockMsgs[0].Content[0].(*types.ContentBlockMemberToolResult)
	require.True(t, ok)
	assert.Equal(t, "tool-1", *toolResult.Value.ToolUseId)
}

func TestConvertMessages_EmptyContent(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: ""},
		{Role: chat.MessageRoleUser, Content: "   "},
	}

	bedrockMsgs, _ := convertMessages(msgs)
	assert.Empty(t, bedrockMsgs)
}

func TestConvertToolConfig(t *testing.T) {
	t.Parallel()

	requestTools := []tools.Tool{{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"arg1": map[string]any{"type": "string"},
			},
		},
	}}

	config := convertToolConfig(requestTools)

	require.NotNil(t, config)
	require.Len(t, config.Tools, 1)

	toolSpec, ok := config.Tools[0].(*types.ToolMemberToolSpec)
	require.True(t, ok)
	assert.Equal(t, "test_tool", *toolSpec.Value.Name)
	assert.Equal(t, "A test tool", *toolSpec.Value.Description)
}

func TestConvertToolConfig_Empty(t *testing.T) {
	t.Parallel()

	config := convertToolConfig(nil)
	assert.Nil(t, config)

	config = convertToolConfig([]tools.Tool{})
	assert.Nil(t, config)
}

func TestGetProviderOpt(t *testing.T) {
	t.Parallel()

	opts := map[string]any{
		"region":   "us-west-2",
		"role_arn": "arn:aws:iam::123:role/Test",
		"number":   42,
	}

	assert.Equal(t, "us-west-2", getProviderOpt[string](opts, "region"))
	assert.Empty(t, getProviderOpt[string](opts, "nonexistent"))
	assert.Empty(t, getProviderOpt[string](nil, "region"))
	assert.Equal(t, 42, getProviderOpt[int](opts, "number"))
}

func TestConvertMessages_MultiContent(t *testing.T) {
	t.Parallel()

	msgs := []chat.Message{{
		Role: chat.MessageRoleUser,
		MultiContent: []chat.MessagePart{
			{Type: chat.MessagePartTypeText, Text: "First part"},
			{Type: chat.MessagePartTypeText, Text: "Second part"},
		},
	}}

	bedrockMsgs, _ := convertMessages(msgs)

	require.Len(t, bedrockMsgs, 1)
	require.Len(t, bedrockMsgs[0].Content, 2)
}

func TestConvertMessages_ConsecutiveToolResults(t *testing.T) {
	t.Parallel()

	// Simulates scenario where assistant calls multiple tools and gets multiple results
	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "Do two things"},
		{
			Role: chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{
				{ID: "tool-1", Function: tools.FunctionCall{Name: "action1", Arguments: "{}"}},
				{ID: "tool-2", Function: tools.FunctionCall{Name: "action2", Arguments: "{}"}},
			},
		},
		{Role: chat.MessageRoleTool, ToolCallID: "tool-1", Content: "Result 1"},
		{Role: chat.MessageRoleTool, ToolCallID: "tool-2", Content: "Result 2"},
		{Role: chat.MessageRoleUser, Content: "Continue"},
	}

	bedrockMsgs, _ := convertMessages(msgs)

	// Expect: user, assistant, user (grouped tool results), user
	require.Len(t, bedrockMsgs, 4)

	// First message: user text
	assert.Equal(t, types.ConversationRoleUser, bedrockMsgs[0].Role)

	// Second message: assistant with tool calls
	assert.Equal(t, types.ConversationRoleAssistant, bedrockMsgs[1].Role)
	require.Len(t, bedrockMsgs[1].Content, 2) // Two tool use blocks

	// Third message: user with GROUPED tool results (critical fix!)
	assert.Equal(t, types.ConversationRoleUser, bedrockMsgs[2].Role)
	require.Len(t, bedrockMsgs[2].Content, 2) // Both tool results in single message

	// Verify both tool results are present
	toolResult1, ok := bedrockMsgs[2].Content[0].(*types.ContentBlockMemberToolResult)
	require.True(t, ok)
	assert.Equal(t, "tool-1", *toolResult1.Value.ToolUseId)

	toolResult2, ok := bedrockMsgs[2].Content[1].(*types.ContentBlockMemberToolResult)
	require.True(t, ok)
	assert.Equal(t, "tool-2", *toolResult2.Value.ToolUseId)

	// Fourth message: user text
	assert.Equal(t, types.ConversationRoleUser, bedrockMsgs[3].Role)
}

func TestBearerTokenTransport(t *testing.T) {
	t.Parallel()

	// Create a test server to capture the Authorization header
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create transport with bearer token
	transport := &bearerTokenTransport{
		token: "test-api-key-12345",
		base:  http.DefaultTransport,
	}

	// Make a request through the transport
	client := &http.Client{Transport: transport}
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify the Authorization header was set correctly
	assert.Equal(t, "Bearer test-api-key-12345", capturedAuth)
}

// Image URL conversion tests

func TestConvertImageURL_NonDataURL(t *testing.T) {
	t.Parallel()

	imageURL := &chat.MessageImageURL{URL: "https://example.com/image.png"}
	result := convertImageURL(imageURL)
	assert.Nil(t, result)
}

func TestConvertImageURL_InvalidDataURLFormat(t *testing.T) {
	t.Parallel()

	// Missing comma separator
	imageURL := &chat.MessageImageURL{URL: "data:image/pngbase64invaliddata"}
	result := convertImageURL(imageURL)
	assert.Nil(t, result)
}

func TestConvertImageURL_InvalidBase64(t *testing.T) {
	t.Parallel()

	imageURL := &chat.MessageImageURL{URL: "data:image/png;base64,not-valid-base64!!!"}
	result := convertImageURL(imageURL)
	assert.Nil(t, result)
}

func TestConvertImageURL_AllFormats(t *testing.T) {
	t.Parallel()

	validBase64 := base64.StdEncoding.EncodeToString([]byte("fake image data"))

	testCases := []struct {
		name           string
		mimeType       string
		expectedFormat types.ImageFormat
	}{
		{"JPEG", "image/jpeg", types.ImageFormatJpeg},
		{"PNG", "image/png", types.ImageFormatPng},
		{"GIF", "image/gif", types.ImageFormatGif},
		{"WebP", "image/webp", types.ImageFormatWebp},
		{"Unknown defaults to JPEG", "image/bmp", types.ImageFormatJpeg},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			imageURL := &chat.MessageImageURL{
				URL: "data:" + tc.mimeType + ";base64," + validBase64,
			}
			result := convertImageURL(imageURL)
			require.NotNil(t, result)

			imageBlock, ok := result.(*types.ContentBlockMemberImage)
			require.True(t, ok)
			assert.Equal(t, tc.expectedFormat, imageBlock.Value.Format)
		})
	}
}

func TestConvertImageURL_ValidImage(t *testing.T) {
	t.Parallel()

	// Create a valid base64-encoded "image"
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	validBase64 := base64.StdEncoding.EncodeToString(imageData)

	imageURL := &chat.MessageImageURL{
		URL: "data:image/png;base64," + validBase64,
	}

	result := convertImageURL(imageURL)
	require.NotNil(t, result)

	imageBlock, ok := result.(*types.ContentBlockMemberImage)
	require.True(t, ok)
	assert.Equal(t, types.ImageFormatPng, imageBlock.Value.Format)

	// Verify decoded data matches
	source, ok := imageBlock.Value.Source.(*types.ImageSourceMemberBytes)
	require.True(t, ok)
	assert.Equal(t, imageData, source.Value)
}

// NewClient validation tests

type mockEnvProvider struct {
	values map[string]string
}

func (m *mockEnvProvider) Get(_ context.Context, key string) string {
	if m.values == nil {
		return ""
	}
	return m.values[key]
}

var _ environment.Provider = (*mockEnvProvider)(nil)

func TestNewClient_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := NewClient(t.Context(), nil, &mockEnvProvider{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model configuration is required")
}

func TestNewClient_WrongProvider(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4",
	}
	_, err := NewClient(t.Context(), cfg, &mockEnvProvider{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model type must be 'bedrock'")
}
