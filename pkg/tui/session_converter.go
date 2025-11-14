package tui

import (
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/types"
)

// ConvertSessionToTUIMessages converts a session's messages to TUI message format for display
func ConvertSessionToTUIMessages(sess *session.Session) []*types.Message {
	if sess == nil {
		return nil
	}

	var tuiMessages []*types.Message

	// Map to track tool call IDs to their results
	toolResults := make(map[string]string)

	// First pass: collect all tool results
	for _, item := range sess.Messages {
		if !item.IsMessage() {
			continue
		}

		msg := item.Message.Message
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID != "" {
			toolResults[msg.ToolCallID] = msg.Content
		}
	}

	// Second pass: convert messages to TUI format
	for _, item := range sess.Messages {
		if !item.IsMessage() {
			// Skip sub-sessions for now
			continue
		}

		msg := item.Message.Message
		agentName := item.Message.AgentName

		// Skip system messages (they're instructions, not conversation)
		if msg.Role == chat.MessageRoleSystem {
			continue
		}

		// Skip implicit messages
		if item.Message.Implicit {
			continue
		}

		switch msg.Role {
		case chat.MessageRoleUser:
			// Convert user messages
			tuiMessages = append(tuiMessages, types.User(msg.Content))

		case chat.MessageRoleAssistant:
			// Handle reasoning content first
			if msg.ReasoningContent != "" {
				tuiMessages = append(tuiMessages, types.Agent(
					types.MessageTypeAssistantReasoning,
					agentName,
					msg.ReasoningContent,
				))
			}

			// Add main assistant message if it has content
			if msg.Content != "" {
				tuiMessages = append(tuiMessages, types.Agent(
					types.MessageTypeAssistant,
					agentName,
					msg.Content,
				))
			}

			// Handle tool calls - create separate tool call messages
			for _, toolCall := range msg.ToolCalls {
				// Determine tool status based on whether we have a result
				var status types.ToolStatus
				var content string

				if result, hasResult := toolResults[toolCall.ID]; hasResult {
					status = types.ToolStatusCompleted
					content = result
				} else {
					// No result means it's pending or never completed
					status = types.ToolStatusPending
					content = ""
				}

				// Create a tool definition placeholder
				// We don't have the full tool definition from the session,
				// so we create a minimal one with the name and description
				toolDef := tools.Tool{
					Name:        toolCall.Function.Name,
					Description: "", // Not available in session data
					Category:    "", // Not available in session data
				}

				toolMsg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
				toolMsg.Content = content
				tuiMessages = append(tuiMessages, toolMsg)
			}

		case chat.MessageRoleTool:
			// Tool results are already handled by matching them to tool calls
			// in the ToolCalls processing above, so we skip them here
			continue
		}
	}

	return tuiMessages
}
