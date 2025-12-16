package messages

import "github.com/docker/cagent/pkg/session"

// Session command messages
type (
	NewSessionMsg             struct{}
	EvalSessionMsg            struct{ Filename string }
	CompactSessionMsg         struct{}
	CopySessionToClipboardMsg struct{}
	ToggleYoloMsg             struct{}
	OpenSessionsDialogMsg     struct{}
	LoadSessionMsg            struct{ Session *session.Session }
)

// AgentCommandMsg command message
type AgentCommandMsg struct {
	Command string
}

// MCPPromptMsg command message
type MCPPromptMsg struct {
	PromptName string
	Arguments  map[string]string
}

// OpenURLMsg is a url for opening message
type OpenURLMsg struct {
	URL string
}

type ShowMCPPromptInputMsg struct {
	PromptName string
	PromptInfo any // mcptools.PromptInfo but avoiding import cycles
}
