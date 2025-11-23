package mcp

// PromptInfo contains metadata about an available MCP prompt
type PromptInfo struct {
	Name        string           `json:"name"`        // The prompt name/identifier
	Description string           `json:"description"` // Human-readable description of what this prompt does
	Arguments   []PromptArgument `json:"arguments"`   // List of arguments this prompt accepts
}

// PromptArgument represents a single argument for an MCP prompt
type PromptArgument struct {
	Name        string `json:"name"`        // The name of the argument
	Description string `json:"description"` // Human-readable description of the argument
	Required    bool   `json:"required"`    // Whether this argument is required
}
