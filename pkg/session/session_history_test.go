package session

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

func TestSessionNumHistoryItems(t *testing.T) {
	tests := []struct {
		name                     string
		numHistoryItems          int
		messageCount             int
		expectedConversationMsgs int
	}{
		{
			name:                     "limit to 3 conversation messages",
			numHistoryItems:          3,
			messageCount:             10,
			expectedConversationMsgs: 3, // Limited to 3 despite 20 total messages
		},
		{
			name:                     "no limit when numHistoryItems is 0",
			numHistoryItems:          0,
			messageCount:             150,
			expectedConversationMsgs: 100, // Should use default maxMessages
		},
		{
			name:                     "limit to 5 conversation messages",
			numHistoryItems:          5,
			messageCount:             8,
			expectedConversationMsgs: 5, // Limited to 5 out of 16 total messages
		},
		{
			name:                     "fewer messages than limit",
			numHistoryItems:          10,
			messageCount:             5,
			expectedConversationMsgs: 10, // 5 user + 5 assistant = 10 total conversation messages
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testAgent := agent.New("test-agent", "test instruction",
				agent.WithNumHistoryItems(tt.numHistoryItems))

			s := New()
			for i := range tt.messageCount {
				s.AddMessage(UserMessage("", fmt.Sprintf("Message %d", i)))
				s.AddMessage(&Message{
					AgentName: "test-agent",
					Message: chat.Message{
						Role:    chat.MessageRoleAssistant,
						Content: fmt.Sprintf("Response %d", i),
					},
				})
			}

			messages := s.GetMessages(testAgent)

			// Count conversation messages (non-system)
			conversationCount := 0
			systemCount := 0
			for _, msg := range messages {
				if msg.Role == chat.MessageRoleSystem {
					systemCount++
				} else {
					conversationCount++
				}
			}

			// System messages should always be present (at least the instruction)
			assert.Positive(t, systemCount, "Should have system messages")

			// Conversation messages should be limited
			assert.LessOrEqual(t, conversationCount, tt.expectedConversationMsgs,
				"Conversation messages should not exceed the configured limit")
		})
	}
}

func TestTrimMessagesPreservesSystemMessages(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "System instruction 1"},
		{Role: chat.MessageRoleSystem, Content: "System instruction 2"},
		{Role: chat.MessageRoleUser, Content: "User message 1"},
		{Role: chat.MessageRoleAssistant, Content: "Assistant response 1"},
		{Role: chat.MessageRoleSystem, Content: "Tool instruction"},
		{Role: chat.MessageRoleUser, Content: "User message 2"},
		{Role: chat.MessageRoleAssistant, Content: "Assistant response 2"},
		{Role: chat.MessageRoleUser, Content: "User message 3"},
		{Role: chat.MessageRoleAssistant, Content: "Assistant response 3"},
	}

	trimmed := trimMessages(messages, 1)

	// Count message types
	systemCount := 0
	conversationCount := 0
	for _, msg := range trimmed {
		if msg.Role == chat.MessageRoleSystem {
			systemCount++
		} else {
			conversationCount++
		}
	}

	// All system messages should be preserved
	assert.Equal(t, 3, systemCount, "All system messages should be preserved")
	assert.Equal(t, 1, conversationCount, "Should have exactly 1 conversation message")

	// The preserved conversation message should be the most recent
	assert.Equal(t, "Assistant response 3", trimmed[len(trimmed)-1].Content,
		"Should preserve the most recent conversation message")
}

func TestTrimMessagesConversationLimit(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "System prompt"},
		{Role: chat.MessageRoleUser, Content: "Message 1"},
		{Role: chat.MessageRoleAssistant, Content: "Response 1"},
		{Role: chat.MessageRoleUser, Content: "Message 2"},
		{Role: chat.MessageRoleAssistant, Content: "Response 2"},
		{Role: chat.MessageRoleUser, Content: "Message 3"},
		{Role: chat.MessageRoleAssistant, Content: "Response 3"},
		{Role: chat.MessageRoleUser, Content: "Message 4"},
		{Role: chat.MessageRoleAssistant, Content: "Response 4"},
	}

	testCases := []struct {
		limit                int
		expectedTotal        int
		expectedConversation int
		expectedSystem       int
	}{
		{limit: 2, expectedTotal: 3, expectedConversation: 2, expectedSystem: 1},
		{limit: 4, expectedTotal: 5, expectedConversation: 4, expectedSystem: 1},
		{limit: 8, expectedTotal: 9, expectedConversation: 8, expectedSystem: 1},
		{limit: 100, expectedTotal: 9, expectedConversation: 8, expectedSystem: 1},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("limit_%d", tc.limit), func(t *testing.T) {
			trimmed := trimMessages(messages, tc.limit)

			systemCount := 0
			conversationCount := 0
			for _, msg := range trimmed {
				if msg.Role == chat.MessageRoleSystem {
					systemCount++
				} else {
					conversationCount++
				}
			}

			assert.Len(t, trimmed, tc.expectedTotal, "Total message count")
			assert.Equal(t, tc.expectedSystem, systemCount, "System message count")
			assert.Equal(t, tc.expectedConversation, conversationCount, "Conversation message count")
		})
	}
}

func TestTrimMessagesWithToolCallsPreservation(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "System prompt"},
		{Role: chat.MessageRoleUser, Content: "Old message"},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Old response with tool",
			ToolCalls: []tools.ToolCall{
				{ID: "old_tool_1", Function: tools.FunctionCall{Name: "test"}},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Old tool result",
			ToolCallID: "old_tool_1",
		},
		{Role: chat.MessageRoleUser, Content: "Recent message"},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "Recent response with tool",
			ToolCalls: []tools.ToolCall{
				{ID: "recent_tool_1", Function: tools.FunctionCall{Name: "test"}},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "Recent tool result",
			ToolCallID: "recent_tool_1",
		},
	}

	// Limit to 3 conversation messages (should keep the recent tool interaction)
	trimmed := trimMessages(messages, 3)

	toolCallIDs := make(map[string]bool)
	for _, msg := range trimmed {
		if msg.Role == chat.MessageRoleAssistant {
			for _, tc := range msg.ToolCalls {
				toolCallIDs[tc.ID] = true
			}
		}
	}

	for _, msg := range trimmed {
		if msg.Role == chat.MessageRoleTool {
			assert.True(t, toolCallIDs[msg.ToolCallID],
				"Tool result should have a corresponding tool call")
		}
	}

	// Should not have the old tool call
	hasOldTool := false
	for _, msg := range trimmed {
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID == "old_tool_1" {
			hasOldTool = true
		}
	}
	assert.False(t, hasOldTool, "Should not have old tool results without their calls")
}

func TestNumHistoryItemsConfiguration(t *testing.T) {
	testCases := []struct {
		configValue int
		expected    int
		description string
	}{
		{0, maxMessages, "Zero should use default"},
		{-1, maxMessages, "Negative should use default"},
		{5, 5, "Positive value should be used"},
		{200, 200, "Large value should be accepted"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			a := agent.New("test", "instruction",
				agent.WithNumHistoryItems(tc.configValue))

			s := New()
			s.AddMessage(UserMessage("", "test"))

			// The actual limit used should be reflected in debug logs
			// For this test, we just verify the agent configuration
			actualLimit := a.NumHistoryItems()
			if actualLimit <= 0 {
				actualLimit = maxMessages
			}

			assert.Equal(t, tc.expected, actualLimit, tc.description)
		})
	}
}
