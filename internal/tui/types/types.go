package types

import "github.com/docker/cagent/pkg/tools"

// MessageType represents different types of messages
type MessageType int

const (
	MessageTypeUser MessageType = iota
	MessageTypeAssistant
	MessageTypeError
	MessageTypeShellOutput
	MessageTypeSeparator
	MessageTypeToolCall
	MessageTypeToolResult
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
	Type       MessageType
	Content    string
	Sender     string         // Agent name for assistant messages
	ToolCall   tools.ToolCall // Associated tool call for tool messages
	ToolStatus ToolStatus     // Status for tool calls
}
