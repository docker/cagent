package providerutil

import "github.com/docker/docker-agent/pkg/chat"

// FinalAssistantPrefill reports whether the conversation ends with an
// assistant message that is not a tool call, i.e. a response prefill.
// Trailing system messages are ignored.
func FinalAssistantPrefill(messages []chat.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case chat.MessageRoleSystem:
			continue
		case chat.MessageRoleAssistant:
			return len(messages[i].ToolCalls) == 0
		default:
			return false
		}
	}
	return false
}
