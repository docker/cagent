package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
)

func PlainText(sess *session.Session) string {
	var builder strings.Builder

	messages := sess.GetAllMessages()
	for i := range messages {
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
			builder.WriteString(string(formatted))
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
