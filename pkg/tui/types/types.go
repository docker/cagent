package types

import "github.com/docker/cagent/pkg/tools"

// MessageType represents different types of messages
type MessageType int

const (
	MessageTypeUser MessageType = iota
	MessageTypeAssistant
	MessageTypeAssistantReasoning
	MessageTypeSpinner
	MessageTypeError
	MessageTypeShellOutput
	MessageTypeSeparator
	MessageTypeToolCall
	MessageTypeToolResult
	MessageTypeSystem
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
	Sender         string         // Agent name for assistant messages
	ToolCall       tools.ToolCall // Associated tool call for tool messages
	ToolDefinition tools.Tool     // Definition of the tool being called
	ToolStatus     ToolStatus     // Status for tool calls
}
