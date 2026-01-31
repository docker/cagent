package tools

import "context"

// Startable is implemented by toolsets that require initialization before use.
// Toolsets that don't implement this interface are assumed to be ready immediately.
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

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

// GetInstructions returns instructions if the toolset implements Instructable.
// Returns empty string if the toolset doesn't provide instructions.
func GetInstructions(ts ToolSet) string {
	if i, ok := As[Instructable](ts); ok {
		return i.Instructions()
	}
	return ""
}

// ConfigureHandlers sets all applicable handlers on a toolset.
// It checks for Elicitable and OAuthCapable interfaces and configures them.
// This is a convenience function that handles the capability checking internally.
func ConfigureHandlers(ts ToolSet, elicitHandler ElicitationHandler, oauthHandler func(), managedOAuth bool) {
	if e, ok := As[Elicitable](ts); ok {
		e.SetElicitationHandler(elicitHandler)
	}
	if o, ok := As[OAuthCapable](ts); ok {
		o.SetOAuthSuccessHandler(oauthHandler)
		o.SetManagedOAuth(managedOAuth)
	}
}
