package oauth

import "context"

// Manager defines the contract for OAuth flow management
type Manager interface {
	// ExecuteWithOAuth wraps an operation that may require OAuth authorization with automatic retry logic.
	// If the operation fails with an OAuth authorization error, it handles the OAuth flow and retries the operation.
	// Returns the error from the operation or from the OAuth flow if it fails.
	ExecuteWithOAuth(ctx context.Context, sessionID string, operation func() error) error

	// StartAuthorizationFlow signals that user confirmation has been given to start the OAuth flow
	StartAuthorizationFlow(ctx context.Context, confirmation bool)

	// SendAuthorizationCode sends the OAuth authorization code and state after user has completed the OAuth flow
	SendAuthorizationCode(ctx context.Context, code, state string) error

	// UpdateEmitCallback updates the callback function for emitting auth events
	UpdateEmitCallback(emitAuthRequired func(serverURL, serverType, status string))

	// Cleanup stops any owned resources like callback servers
	Cleanup(ctx context.Context) error
}
