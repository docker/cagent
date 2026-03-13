package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin"
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

	// Both user messages are protected, so result includes them plus the most recent assistant/tool pair
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

	s.AddMessage(NewAgentMessage("", &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "test message",
	}))

	s.AddMessage(NewAgentMessage("", &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "using tool",
		ToolCalls: []tools.ToolCall{
			{
				ID: "test-tool",
			},
		},
	}))

	s.AddMessage(NewAgentMessage("", &chat.Message{
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

	s.AddMessage(NewAgentMessage("", &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "first message",
	}))
	s.AddMessage(NewAgentMessage("", &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "first response",
	}))

	s.Messages = append(s.Messages, Item{Summary: "This is a summary of the conversation so far"})

	s.AddMessage(NewAgentMessage("", &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "message after summary",
	}))
	s.AddMessage(NewAgentMessage("", &chat.Message{
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
		if msg.Role == chat.MessageRoleUser && msg.Content == "Session Summary: This is a summary of the conversation so far" {
			summaryFound = true
		}
	}

	// We should have:
	// - 1 summary user message
	// - 2 messages after the summary (user + assistant)
	// - Various other system messages from agent setup
	assert.True(t, summaryFound, "should include summary as user message")
	assert.Equal(t, 3, userAssistantMessages, "should only include messages after summary")
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

	assert.Contains(t, messages[1].Content, "Todo Tools")
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
	assert.Contains(t, messages[checkpointIndices[0]].Content, "Todo Tools", "checkpoint #1 should be on toolset instructions")

	// Verify checkpoint #2 is on date
	assert.Contains(t, messages[checkpointIndices[1]].Content, "Today's date", "checkpoint #2 should be on date message")
}

func TestGetLastUserMessages(t *testing.T) {
	t.Parallel()

	t.Run("empty session returns empty slice", func(t *testing.T) {
		t.Parallel()
		s := New()
		assert.Empty(t, s.GetLastUserMessages(2))
	})

	t.Run("session with fewer messages than requested returns all", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddMessage(NewAgentMessage("", &chat.Message{
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
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "First",
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Response 1",
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Second",
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Response 2",
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
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
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "First",
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "   ", // Empty after trim
		}))
		s.AddMessage(NewAgentMessage("", &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "Third",
		}))

		msgs := s.GetLastUserMessages(2)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "First", msgs[0])
		assert.Equal(t, "Third", msgs[1])
	})
}

func TestEvalCriteriaUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    EvalCriteria
		wantErr bool
	}{
		{
			name:  "valid fields",
			input: `{"relevance":["is correct"],"size":"M","setup":"echo hello","working_dir":"mydir"}`,
			want: EvalCriteria{
				Relevance:  []string{"is correct"},
				Size:       "M",
				Setup:      "echo hello",
				WorkingDir: "mydir",
			},
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  EvalCriteria{},
		},
		{
			name:    "unknown field rejected",
			input:   `{"relevance":[],"unknown_field":"value"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got EvalCriteria
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTransferTaskPromptExcludesParents(t *testing.T) {
	t.Parallel()

	// Build hierarchy: planner -> root -> librarian
	// root's sub-agents: [librarian]
	// root's parents: [planner] (set by planner listing root as a sub-agent)
	librarian := agent.New("librarian", "", agent.WithDescription("Library agent"))
	root := agent.New("root", "You are the root agent",
		agent.WithDescription("Root agent"),
	)
	planner := agent.New("planner", "",
		agent.WithDescription("Planner agent"),
	)
	// Connect: root -> librarian (root has librarian as sub-agent)
	agent.WithSubAgents(librarian)(root)
	// Connect: planner -> root (planner has root as sub-agent, making root's parent = planner)
	agent.WithSubAgents(root)(planner)

	// Verify parent relationship was established
	require.Len(t, root.Parents(), 1)
	assert.Equal(t, "planner", root.Parents()[0].Name())

	s := New()
	messages := s.GetMessages(root)

	// Find the system message about sub-agents
	var subAgentMsg string
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleSystem && strings.Contains(msg.Content, "transfer_task") {
			subAgentMsg = msg.Content
			break
		}
	}

	require.NotEmpty(t, subAgentMsg, "should have a sub-agent system message")
	assert.Contains(t, subAgentMsg, "librarian", "should list librarian as a valid sub-agent")
	assert.NotContains(t, subAgentMsg, "planner", "should NOT list parent agent planner as a valid transfer target")
}
