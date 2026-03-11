package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

func PlainText(sess *session.Session) string {
	var builder strings.Builder

	// Make a copy of the session items to avoid race conditions
	// Messages is a public field, so we can access it directly
	items := make([]session.Item, len(sess.Messages))
	copy(items, sess.Messages)

	// Find the last summary in the session
	lastSummaryIndex := -1
	var summary string
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Summary != "" {
			lastSummaryIndex = i
			summary = items[i].Summary
			break
		}
	}

	// If a summary exists, start with it
	if lastSummaryIndex >= 0 {
		fmt.Fprintf(&builder, "## Session Summary\n\n%s\n", summary)
	}

	// Get all messages
	messages := sess.GetAllMessages()

	// If we have a summary, we need to skip messages that were summarized
	// We do this by tracking message indices and only including messages after the summary
	var startMessageIndex int
	if lastSummaryIndex >= 0 {
		// Count how many messages come before the summary
		messageCount := 0
		for i := 0; i <= lastSummaryIndex; i++ {
			if items[i].IsMessage() {
				messageCount++
			} else if items[i].IsSubSession() {
				// Count all messages in the sub-session
				messageCount += len(items[i].SubSession.GetAllMessages())
			}
		}
		startMessageIndex = messageCount
	}

	// Write messages (starting after the summary if one exists)
	for i := startMessageIndex; i < len(messages); i++ {
		msg := messages[i]

		if msg.Implicit {
			continue
		}

		switch msg.Message.Role {
		case chat.MessageRoleUser:
			writeUserMessage(&builder, msg)
		case chat.MessageRoleAssistant:
			writeAssistantMessage(&builder, msg)
		case chat.MessageRoleTool:
			writeToolMessage(&builder, msg)
		}
	}

	return strings.TrimSpace(builder.String())
}

func writeUserMessage(builder *strings.Builder, msg session.Message) {
	fmt.Fprintf(builder, "\n## User\n\n%s\n", msg.Message.Content)
}

func writeAssistantMessage(builder *strings.Builder, msg session.Message) {
	builder.WriteString("\n## Assistant")
	if msg.AgentName != "" {
		fmt.Fprintf(builder, " (%s)", msg.AgentName)
	}
	builder.WriteString("\n\n")

	if msg.Message.ReasoningContent != "" {
		builder.WriteString("### Reasoning\n\n")
		builder.WriteString(msg.Message.ReasoningContent)
		builder.WriteString("\n\n")
	}

	if msg.Message.Content != "" {
		builder.WriteString(msg.Message.Content)
		builder.WriteString("\n")
	}

	if len(msg.Message.ToolCalls) > 0 {
		builder.WriteString("\n### Tool Calls\n\n")
		for _, toolCall := range msg.Message.ToolCalls {
			fmt.Fprintf(builder, "- **%s**", toolCall.Function.Name)
			if toolCall.ID != "" {
				fmt.Fprintf(builder, " (ID: %s)", toolCall.ID)
			}

			builder.WriteString("\n")
			toJSONString(builder, toolCall.Function.Arguments)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
}

func writeToolMessage(builder *strings.Builder, msg session.Message) {
	builder.WriteString("### Tool Result")
	if msg.Message.ToolCallID != "" {
		fmt.Fprintf(builder, " (ID: %s)", msg.Message.ToolCallID)
	}
	fmt.Fprintf(builder, "\n\n")

	toJSONString(builder, msg.Message.Content)
	builder.WriteString("\n")
}

func toJSONString(builder *strings.Builder, in string) {
	var content any
	if err := json.Unmarshal([]byte(in), &content); err == nil {
		if formatted, err := json.MarshalIndent(content, "", "  "); err == nil {
			builder.WriteString("```json\n")
			builder.Write(formatted)
			builder.WriteString("\n```\n")
		} else {
			builder.WriteString(in)
			builder.WriteString("\n")
		}
	} else {
		if in != "" {
			builder.WriteString(in)
			builder.WriteString("\n")
		}
	}
}
