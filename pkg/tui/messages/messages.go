package messages

// Session command messages
type (
	NewSessionMsg             struct{}
	EvalSessionMsg            struct{}
	CompactSessionMsg         struct{}
	CopySessionToClipboardMsg struct{}
	ToggleYoloMsg             struct{}
)

// Agent command message
type AgentCommandMsg struct {
	Command string
}

// MCP Prompt command message
type MCPPromptMsg struct {
	PromptName string
	Arguments  map[string]string
}

// URL opening message
type OpenURLMsg struct {
	URL string
}

// Dialog messages
type ShowMCPPromptInputMsg struct {
	PromptName string
	PromptInfo any // mcptools.PromptInfo but avoiding import cycles
}
