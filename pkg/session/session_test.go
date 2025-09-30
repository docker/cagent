package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

func TestTrimMessages(t *testing.T) {
	// Create a slice of messages that exceeds maxMessages
	messages := make([]chat.Message, maxMessages+10)

	// Fill with some basic messages
	for i := range messages {
		messages[i] = chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "message",
		}
	}

	// Test basic trimming
	result := trimMessages(messages, maxMessages)
	assert.Len(t, result, maxMessages, "should trim to maxMessages")
}

func TestTrimMessagesWithToolCalls(t *testing.T) {
	messages := []chat.Message{
		{
			Role:    chat.MessageRoleUser,
			Content: "first message",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "response with tool",
			ToolCalls: []tools.ToolCall{
				{
					ID: "tool1",
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "tool result",
			ToolCallID: "tool1",
		},
		{
			Role:    chat.MessageRoleUser,
			Content: "second message",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "another response",
			ToolCalls: []tools.ToolCall{
				{
					ID: "tool2",
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "tool result 2",
			ToolCallID: "tool2",
		},
	}

	// Use 3 as the limit to force trimming
	maxItems := 3

	result := trimMessages(messages, maxItems)

	// Should keep last 3 messages, but ensure tool call consistency
	assert.Len(t, result, maxItems)

	// Verify we don't have any orphaned tool results
	toolCalls := make(map[string]bool)
	for _, msg := range result {
		if msg.Role == chat.MessageRoleAssistant {
			for _, tool := range msg.ToolCalls {
				toolCalls[tool.ID] = true
			}
		}
		if msg.Role == chat.MessageRoleTool {
			assert.True(t, toolCalls[msg.ToolCallID], "tool result should have corresponding assistant message")
		}
	}
}

func TestGetMessages(t *testing.T) {
	// Create a test agent
	testAgent := &agent.Agent{}

	// Create a session with many messages
	s := New()

	// Add more than maxMessages to the session
	for i := 0; i < maxMessages+10; i++ {
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "test message",
		}))
	}

	// Get messages for the agent
	messages := s.GetMessages(testAgent)

	// Count non-system messages (since system messages are not limited)
	nonSystemCount := 0
	for _, msg := range messages {
		if msg.Role != chat.MessageRoleSystem {
			nonSystemCount++
		}
	}

	// Verify we get at most maxMessages for non-system messages
	assert.LessOrEqual(t, nonSystemCount, maxMessages, "non-system messages should not exceed maxMessages")
}

func TestGetMessagesWithToolCalls(t *testing.T) {
	// Create a test agent
	testAgent := &agent.Agent{}

	// Create a session
	s := New()

	// Add a sequence of messages with tool calls
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "test message",
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "using tool",
		ToolCalls: []tools.ToolCall{
			{
				ID: "test-tool",
			},
		},
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "test-tool",
		Content:    "tool result",
	}))

	// Set maxMessages to 2 to force trimming
	oldMax := maxMessages
	maxMessages = 2
	defer func() { maxMessages = oldMax }()

	messages := s.GetMessages(testAgent)

	// Verify tool call consistency
	toolCalls := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleAssistant {
			for _, tool := range msg.ToolCalls {
				toolCalls[tool.ID] = true
			}
		}
		if msg.Role == chat.MessageRoleTool {
			assert.True(t, toolCalls[msg.ToolCallID], "tool result should have corresponding assistant message")
		}
	}
}

func TestGetMessagesWithSummary(t *testing.T) {
	// Create a test agent
	testAgent := &agent.Agent{}

	// Create a session
	s := New()

	// Add some initial messages
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "first message",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "first response",
	}))

	// Add a summary
	s.Messages = append(s.Messages, Item{Summary: "This is a summary of the conversation so far"})

	// Add messages after the summary
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "message after summary",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "response after summary",
	}))

	// Get messages
	messages := s.GetMessages(testAgent)

	// Count non-system messages (user and assistant only)
	userAssistantMessages := 0
	summaryFound := false
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleUser || msg.Role == chat.MessageRoleAssistant {
			userAssistantMessages++
		}
		if msg.Role == chat.MessageRoleSystem && msg.Content == "Session Summary: This is a summary of the conversation so far" {
			summaryFound = true
		}
	}

	// We should have:
	// - 1 summary system message
	// - 2 messages after the summary (user + assistant)
	// - Various other system messages from agent setup
	assert.True(t, summaryFound, "should include summary as system message")
	assert.Equal(t, 2, userAssistantMessages, "should only include messages after summary")
}
