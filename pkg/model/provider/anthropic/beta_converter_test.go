package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

func TestConvertBetaMessages_MergesConsecutiveToolMessages(t *testing.T) {
	// Simulates the roast battle scenario where:
	// - Assistant message has 2 tool_use blocks (transfer_task calls)
	// - Two separate tool messages follow (one for each transfer_task result)
	// - These should be merged into a single user message with 2 tool_result blocks

	messages := []chat.Message{
		{
			Role:    chat.MessageRoleUser,
			Content: "Start the roast battle",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Let me transfer tasks to the comedians",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "tool_call_1",
					Type: "function",
					Function: tools.FunctionCall{
						Name:      "transfer_task",
						Arguments: `{"agent":"roaster_a","task":"Write roast"}`,
					},
				},
				{
					ID:   "tool_call_2",
					Type: "function",
					Function: tools.FunctionCall{
						Name:      "transfer_task",
						Arguments: `{"agent":"roaster_b","task":"Write counter-roast"}`,
					},
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Roast A completed",
			ToolCallID: "tool_call_1",
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Roast B completed",
			ToolCallID: "tool_call_2",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Both roasts are complete!",
		},
	}

	// Convert to Beta format
	betaMessages := convertBetaMessages(messages)

	require.Len(t, betaMessages, 4, "Should have 4 messages after conversion")

	msg0Map, _ := marshalToMapBeta(betaMessages[0])
	msg1Map, _ := marshalToMapBeta(betaMessages[1])
	msg2Map, _ := marshalToMapBeta(betaMessages[2])
	msg3Map, _ := marshalToMapBeta(betaMessages[3])
	assert.Equal(t, "user", msg0Map["role"])
	assert.Equal(t, "assistant", msg1Map["role"])
	assert.Equal(t, "user", msg2Map["role"])
	assert.Equal(t, "assistant", msg3Map["role"])

	userMsg2Map, ok := marshalToMapBeta(betaMessages[2])
	require.True(t, ok)
	content := contentArrayBeta(userMsg2Map)
	require.Len(t, content, 2, "User message should have 2 tool_result blocks")

	toolResultIDs := collectToolResultIDs(content)
	assert.Contains(t, toolResultIDs, "tool_call_1")
	assert.Contains(t, toolResultIDs, "tool_call_2")

	// Most importantly: validate that the sequence is valid for Anthropic API
	err := validateAnthropicSequencingBeta(betaMessages)
	require.NoError(t, err, "Converted messages should pass Anthropic sequencing validation")
}

func TestConvertBetaMessages_SingleToolMessage(t *testing.T) {
	// When there's only one tool message, it should still work correctly
	messages := []chat.Message{
		{
			Role:    chat.MessageRoleUser,
			Content: "Test",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "tool_1",
					Type: "function",
					Function: tools.FunctionCall{
						Name:      "test_tool",
						Arguments: `{}`,
					},
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Tool result",
			ToolCallID: "tool_1",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Done",
		},
	}

	betaMessages := convertBetaMessages(messages)
	require.Len(t, betaMessages, 4)

	// Validate sequence
	err := validateAnthropicSequencingBeta(betaMessages)
	require.NoError(t, err)
}

func TestConvertBetaMessages_NonConsecutiveToolMessages(t *testing.T) {
	// When tool messages are separated by other messages (edge case)
	// Each tool message group should be handled independently
	messages := []chat.Message{
		{
			Role:    chat.MessageRoleUser,
			Content: "First request",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "tool_1",
					Type: "function",
					Function: tools.FunctionCall{
						Name:      "test_tool",
						Arguments: `{}`,
					},
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Tool result 1",
			ToolCallID: "tool_1",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Intermediate response",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "tool_2",
					Type: "function",
					Function: tools.FunctionCall{
						Name:      "test_tool",
						Arguments: `{}`,
					},
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Tool result 2",
			ToolCallID: "tool_2",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Final response",
		},
	}

	betaMessages := convertBetaMessages(messages)

	// Validate the entire sequence
	err := validateAnthropicSequencingBeta(betaMessages)
	require.NoError(t, err, "Messages with non-consecutive tool calls should still validate")
}
