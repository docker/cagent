package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Instructable is implemented by toolsets that provide custom instructions.
type Instructable interface {
	Instructions() string
}

// Elicitable is implemented by toolsets that support MCP elicitation.
type Elicitable interface {
	SetElicitationHandler(handler ElicitationHandler)
}

// OAuthCapable is implemented by toolsets that support OAuth flows.
type OAuthCapable interface {
	SetOAuthSuccessHandler(handler func())
	SetManagedOAuth(managed bool)
}

// PromptProvider is implemented by toolsets that expose MCP prompts.
type PromptProvider interface {
	ListPrompts(ctx context.Context) ([]mcp.Prompt, error)
	GetPrompt(ctx context.Context, name string, arguments map[string]string) (*mcp.GetPromptResult, error)
}

// GetInstructions returns instructions if the toolset implements Instructable.
// Returns empty string if the toolset doesn't provide instructions.
func GetInstructions(ts ToolSet) string {
	if i, ok := As[Instructable](ts); ok {
		return i.Instructions()
	}
	return ""
}
