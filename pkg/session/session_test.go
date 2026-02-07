package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

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

func TestGetMessagesWithToolCalls(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

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

	messages := s.GetMessages(testAgent)

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
	testAgent := &agent.Agent{}

	s := New()

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "first message",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "first response",
	}))

	s.Messages = append(s.Messages, Item{Summary: "This is a summary of the conversation so far"})

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "message after summary",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "response after summary",
	}))

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

func TestGetMessages_Instructions(t *testing.T) {
	testAgent := agent.New("root", "instructions")

	s := New()
	messages := s.GetMessages(testAgent)

	assert.Len(t, messages, 1)
	assert.Equal(t, "instructions", messages[0].Content)
	assert.True(t, messages[0].CacheControl)
}

func TestGetMessages_CacheControl(t *testing.T) {
	testAgent := agent.New("root", "instructions", agent.WithToolSets(&builtin.TodoTool{}))

	s := New()
	messages := s.GetMessages(testAgent)

	assert.Len(t, messages, 2)
	assert.Equal(t, "instructions", messages[0].Content)
	assert.False(t, messages[0].CacheControl)

	assert.Contains(t, messages[1].Content, "Using the Todo Tools")
	assert.True(t, messages[1].CacheControl)
}

func TestGetMessages_CacheControlWithSummary(t *testing.T) {
	// Create agent with invariant, context-specific, and session summary
	testAgent := agent.New("root", "instructions",
		agent.WithToolSets(&builtin.TodoTool{}),
		agent.WithAddDate(true),
	)

	s := New()
	s.Messages = append(s.Messages, Item{Summary: "Test summary"})
	messages := s.GetMessages(testAgent)

	// Should have: instructions, toolset instructions, date, summary
	// Checkpoint #1: last invariant message (toolset instructions)
	// Checkpoint #2: last context-specific message (date)
	// Checkpoint #3: last system message (summary)

	var checkpointIndices []int
	for i, msg := range messages {
		if msg.Role == chat.MessageRoleSystem && msg.CacheControl {
			checkpointIndices = append(checkpointIndices, i)
		}
	}

	// Verify we have 2 checkpoints
	assert.Len(t, checkpointIndices, 2, "should have 2 checkpoints")

	// Verify checkpoint #1 is on toolset instructions
	assert.Contains(t, messages[checkpointIndices[0]].Content, "Using the Todo Tools", "checkpoint #1 should be on toolset instructions")

	// Verify checkpoint #2 is on date
	assert.Contains(t, messages[checkpointIndices[1]].Content, "Today's date", "checkpoint #2 should be on date message")
}

func TestUpdateLastAssistantMessageUsage(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	// Add user message
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "hello",
	}))

	// Add assistant message without usage
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "response",
	}))

	// Update the last assistant message with usage data
	usage := &chat.Usage{
		InputTokens:       100,
		OutputTokens:      50,
		CachedInputTokens: 10,
	}
	s.UpdateLastAssistantMessageUsage(usage, 0.005, "gpt-4")

	// Verify the update
	messages := s.GetAllMessages()
	assert.Len(t, messages, 2)

	lastMsg := messages[1]
	assert.Equal(t, chat.MessageRoleAssistant, lastMsg.Message.Role)
	assert.NotNil(t, lastMsg.Message.Usage)
	assert.Equal(t, int64(100), lastMsg.Message.Usage.InputTokens)
	assert.Equal(t, int64(50), lastMsg.Message.Usage.OutputTokens)
	assert.Equal(t, int64(10), lastMsg.Message.Usage.CachedInputTokens)
	assert.InEpsilon(t, 0.005, lastMsg.Message.Cost, 0.0001)
	assert.Equal(t, "gpt-4", lastMsg.Message.Model)
}

func TestUpdateLastAssistantMessageUsage_NoAssistantMessage(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	// Add only user message
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "hello",
	}))

	// Should not panic when no assistant message exists
	usage := &chat.Usage{InputTokens: 100}
	s.UpdateLastAssistantMessageUsage(usage, 0.01, "model")

	// Verify nothing changed
	messages := s.GetAllMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, chat.MessageRoleUser, messages[0].Message.Role)
}

func TestUpdateLastAssistantMessageUsage_UpdatesOnlyLast(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	// Add multiple assistant messages
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "first response",
		Usage:   &chat.Usage{InputTokens: 10},
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "follow up",
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "second response",
	}))

	// Update usage - should only affect the last assistant message
	usage := &chat.Usage{InputTokens: 200}
	s.UpdateLastAssistantMessageUsage(usage, 0.02, "new-model")

	// Verify only the last assistant message was updated
	messages := s.GetAllMessages()
	assert.Len(t, messages, 3)

	// First assistant message should keep original usage
	assert.NotNil(t, messages[0].Message.Usage)
	assert.Equal(t, int64(10), messages[0].Message.Usage.InputTokens)

	// Last assistant message should have new usage
	assert.NotNil(t, messages[2].Message.Usage)
	assert.Equal(t, int64(200), messages[2].Message.Usage.InputTokens)
	assert.InEpsilon(t, 0.02, messages[2].Message.Cost, 0.0001)
	assert.Equal(t, "new-model", messages[2].Message.Model)
}

func TestGetLastUserMessages(t *testing.T) {
	t.Parallel()

	testAgent := &agent.Agent{}

	t.Run("empty session returns empty slice", func(t *testing.T) {
		t.Parallel()
		s := New()
		assert.Empty(t, s.GetLastUserMessages(2))
	})

	t.Run("session with fewer messages than requested returns all", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Only message",
		}))
		msgs := s.GetLastUserMessages(2)
		assert.Len(t, msgs, 1)
		assert.Equal(t, "Only message", msgs[0])
	})

	t.Run("session returns last n user messages in order", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "First",
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Response 1",
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Second",
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Response 2",
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Third",
		}))

		msgs := s.GetLastUserMessages(2)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "Second", msgs[0]) // Ordered oldest to newest
		assert.Equal(t, "Third", msgs[1])
	})

	t.Run("skips empty user messages", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "First",
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "   ", // Empty after trim
		}))
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Third",
		}))

		msgs := s.GetLastUserMessages(2)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "First", msgs[0])
		assert.Equal(t, "Third", msgs[1])
	})
}
