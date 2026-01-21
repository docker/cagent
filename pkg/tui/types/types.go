package types

import (
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

// MessageType represents different types of messages
type MessageType int

const (
	MessageTypeUser MessageType = iota
	MessageTypeAssistant
	MessageTypeAssistantReasoningBlock // Collapsed reasoning + tool calls block
	MessageTypeSpinner
	MessageTypeError
	MessageTypeShellOutput
	MessageTypeCancelled
	MessageTypeToolCall
	MessageTypeToolResult
	MessageTypeWelcome
	MessageTypeLoading
)

// ToolStatus represents the status of a tool call
type ToolStatus int

const (
	ToolStatusPending ToolStatus = iota
	ToolStatusConfirmation
	ToolStatusRunning
	ToolStatusCompleted
	ToolStatusError
)

// Message represents a single message in the chat
type Message struct {
	Type           MessageType
	Content        string
	Sender         string                // Agent name for assistant messages
	ToolCall       tools.ToolCall        // Associated tool call for tool messages
	ToolDefinition tools.Tool            // Definition of the tool being called
	ToolStatus     ToolStatus            // Status for tool calls
	ToolResult     *tools.ToolCallResult // Result of tool call (when completed)
}

func Agent(typ MessageType, agentName, content string) *Message {
	return &Message{
		Type:    typ,
		Sender:  agentName,
		Content: strings.ReplaceAll(content, "\t", "    "),
	}
}

func ShellOutput(content string) *Message {
	return &Message{
		Type:    MessageTypeShellOutput,
		Content: strings.ReplaceAll(content, "\t", "    "),
	}
}

func Spinner() *Message {
	return &Message{
		Type: MessageTypeSpinner,
	}
}

func Error(content string) *Message {
	return &Message{
		Type:    MessageTypeError,
		Content: strings.ReplaceAll(content, "\t", "    "),
	}
}

func User(content string) *Message {
	return &Message{
		Type:    MessageTypeUser,
		Content: strings.ReplaceAll(content, "\t", "    "),
	}
}

func Cancelled() *Message {
	return &Message{
		Type: MessageTypeCancelled,
	}
}

func Welcome(content string) *Message {
	return &Message{
		Type:    MessageTypeWelcome,
		Content: strings.ReplaceAll(content, "\t", "    "),
	}
}

func ToolCallMessage(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status ToolStatus) *Message {
	return &Message{
		Type:           MessageTypeToolCall,
		Sender:         agentName,
		ToolCall:       toolCall,
		ToolDefinition: toolDef,
		ToolStatus:     status,
	}
}

func Loading(description string) *Message {
	return &Message{
		Type:    MessageTypeLoading,
		Content: strings.ReplaceAll(description, "\t", "    "),
	}
}
