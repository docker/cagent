package oauth

import (
	"errors"
	"fmt"

	"github.com/docker/cagent/pkg/agent"
	"github.com/mark3labs/mcp-go/client"
)

// AuthorizationRequiredError wraps an OAuth authorization error with server information
type AuthorizationRequiredError struct {
	Err        error
	ServerURL  string
	ServerType string
}

func (e *AuthorizationRequiredError) Error() string {
	return fmt.Sprintf("OAuth authorization required for %s server '%s': %v", e.ServerType, e.ServerURL, e.Err)
}

func (e *AuthorizationRequiredError) Unwrap() error {
	return e.Err
}

// MayBeOAuthError checks if the given error is an OAuth authorization error and wraps it
// with server information if available.
//
// This function examines error chains to identify OAuth authorization errors wrapped within
// agent ToolSetError (or any other future error that might contain a possible OAuth error).
// When found, it extracts server information and returns a properly wrapped AuthorizationRequiredError.
//
// Returns:
// - wrapped error and true if this is an OAuth authorization error
// - original error and false otherwise
func MayBeOAuthError(err error) (error, bool) {
	if err != nil {
		// Check if this is a ToolSetError
		var toolSetErr *agent.ToolSetError
		if errors.As(err, &toolSetErr) {
			toolSet := toolSetErr.Toolset
			// Check if the inner error is an OAuth authorization error
			if client.IsOAuthAuthorizationRequiredError(toolSetErr.Err) {
				// Only remote MCP toolsets support OAuth. Remote MCP toolsets implement ServerInfoProvider.
				if serverInfoProvider, ok := toolSet.(ServerInfoProvider); ok {
					serverURL, serverType := serverInfoProvider.GetServerInfo()
					return &AuthorizationRequiredError{
						Err:        toolSetErr.Err,
						ServerURL:  serverURL,
						ServerType: serverType,
					}, true
				}
			}
		}
	}

	return err, false
}
