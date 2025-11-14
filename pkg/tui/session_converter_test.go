package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/types"
)

func TestConvertSessionToTUIMessages(t *testing.T) {
	t.Run("nil session returns nil", func(t *testing.T) {
		result := ConvertSessionToTUIMessages(nil)
		assert.Nil(t, result)
	})

	t.Run("empty session returns empty slice", func(t *testing.T) {
		sess := session.New()
		result := ConvertSessionToTUIMessages(sess)
		assert.Empty(t, result)
	})

	t.Run("converts user message", func(t *testing.T) {
		sess := session.New()
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "",
			Message: chat.Message{
				Role:    chat.MessageRoleUser,
				Content: "Hello, world!",
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 1)
		assert.Equal(t, types.MessageTypeUser, result[0].Type)
		assert.Equal(t, "Hello, world!", result[0].Content)
	})

	t.Run("skips implicit user message", func(t *testing.T) {
		sess := session.New()
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "",
			Message: chat.Message{
				Role:    chat.MessageRoleUser,
				Content: "Hidden message",
			},
			Implicit: true,
		})

		result := ConvertSessionToTUIMessages(sess)
		assert.Empty(t, result)
	})

	t.Run("converts assistant message", func(t *testing.T) {
		sess := session.New()
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "TestAgent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "I can help with that.",
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 1)
		assert.Equal(t, types.MessageTypeAssistant, result[0].Type)
		assert.Equal(t, "TestAgent", result[0].Sender)
		assert.Equal(t, "I can help with that.", result[0].Content)
	})

	t.Run("converts reasoning content", func(t *testing.T) {
		sess := session.New()
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "TestAgent",
			Message: chat.Message{
				Role:             chat.MessageRoleAssistant,
				ReasoningContent: "Let me think about this...",
				Content:          "Here's my answer.",
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 2)

		// First message should be reasoning
		assert.Equal(t, types.MessageTypeAssistantReasoning, result[0].Type)
		assert.Equal(t, "TestAgent", result[0].Sender)
		assert.Equal(t, "Let me think about this...", result[0].Content)

		// Second message should be the response
		assert.Equal(t, types.MessageTypeAssistant, result[1].Type)
		assert.Equal(t, "TestAgent", result[1].Sender)
		assert.Equal(t, "Here's my answer.", result[1].Content)
	})

	t.Run("converts tool calls with results", func(t *testing.T) {
		sess := session.New()

		toolCallID := "call_123"

		// Add assistant message with tool call
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "TestAgent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "",
				ToolCalls: []tools.ToolCall{
					{
						ID:   toolCallID,
						Type: "function",
						Function: tools.FunctionCall{
							Name:      "search_files",
							Arguments: `{"pattern": "*.go"}`,
						},
					},
				},
			},
		})

		// Add tool result
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "TestAgent",
			Message: chat.Message{
				Role:       chat.MessageRoleTool,
				Content:    "Found 10 files",
				ToolCallID: toolCallID,
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 1)

		// Should have one tool call message with completed status
		assert.Equal(t, types.MessageTypeToolCall, result[0].Type)
		assert.Equal(t, "TestAgent", result[0].Sender)
		assert.Equal(t, "search_files", result[0].ToolCall.Function.Name)
		assert.Equal(t, types.ToolStatusCompleted, result[0].ToolStatus)
		assert.Equal(t, "Found 10 files", result[0].Content)
	})

	t.Run("converts tool calls without results", func(t *testing.T) {
		sess := session.New()

		// Add assistant message with tool call but no result
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "TestAgent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "",
				ToolCalls: []tools.ToolCall{
					{
						ID:   "call_456",
						Type: "function",
						Function: tools.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path": "test.txt"}`,
						},
					},
				},
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 1)

		// Should have one tool call message with pending status
		assert.Equal(t, types.MessageTypeToolCall, result[0].Type)
		assert.Equal(t, "TestAgent", result[0].Sender)
		assert.Equal(t, "read_file", result[0].ToolCall.Function.Name)
		assert.Equal(t, types.ToolStatusPending, result[0].ToolStatus)
		assert.Empty(t, result[0].Content)
	})

	t.Run("skips system messages", func(t *testing.T) {
		sess := session.New()
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "",
			Message: chat.Message{
				Role:    chat.MessageRoleSystem,
				Content: "You are a helpful assistant.",
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		assert.Empty(t, result)
	})

	t.Run("complex conversation", func(t *testing.T) {
		sess := session.New()

		// User asks a question
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			Message: chat.Message{
				Role:    chat.MessageRoleUser,
				Content: "What files are in the current directory?",
			},
		})

		// Assistant uses a tool
		toolCallID := "call_789"
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "FileAgent",
			Message: chat.Message{
				Role: chat.MessageRoleAssistant,
				ToolCalls: []tools.ToolCall{
					{
						ID:   toolCallID,
						Type: "function",
						Function: tools.FunctionCall{
							Name:      "list_files",
							Arguments: `{"path": "."}`,
						},
					},
				},
			},
		})

		// Tool returns result
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "FileAgent",
			Message: chat.Message{
				Role:       chat.MessageRoleTool,
				Content:    "file1.txt, file2.txt, file3.txt",
				ToolCallID: toolCallID,
			},
		})

		// Assistant responds
		sess.AddMessage(&session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "FileAgent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "I found three files: file1.txt, file2.txt, and file3.txt.",
			},
		})

		result := ConvertSessionToTUIMessages(sess)
		require.Len(t, result, 3)

		// User message
		assert.Equal(t, types.MessageTypeUser, result[0].Type)
		assert.Equal(t, "What files are in the current directory?", result[0].Content)

		// Tool call
		assert.Equal(t, types.MessageTypeToolCall, result[1].Type)
		assert.Equal(t, "FileAgent", result[1].Sender)
		assert.Equal(t, "list_files", result[1].ToolCall.Function.Name)
		assert.Equal(t, types.ToolStatusCompleted, result[1].ToolStatus)
		assert.Equal(t, "file1.txt, file2.txt, file3.txt", result[1].Content)

		// Assistant response
		assert.Equal(t, types.MessageTypeAssistant, result[2].Type)
		assert.Equal(t, "FileAgent", result[2].Sender)
		assert.Equal(t, "I found three files: file1.txt, file2.txt, and file3.txt.", result[2].Content)
	})
}
