package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
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
