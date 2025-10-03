package oauth

import "context"

// Manager defines the contract for OAuth flow management
type Manager interface {
	// HandleAuthorizationFlow handles a single OAuth authorization flow
	HandleAuthorizationFlow(ctx context.Context, sessionID string, oauthErr *AuthorizationRequiredError) error

	// StartAuthorizationFlow signals that user confirmation has been given to start the OAuth flow
	StartAuthorizationFlow(ctx context.Context, confirmation bool)

	// SendAuthorizationCode sends the OAuth authorization code and state after user has completed the OAuth flow
	SendAuthorizationCode(ctx context.Context, code, state string) error

	// UpdateEmitCallback updates the callback function for emitting auth events
	UpdateEmitCallback(emitAuthRequired func(serverURL, serverType, status string))

	// Cleanup stops any owned resources like callback servers
	Cleanup(ctx context.Context) error
}

// ServerInfoProvider interface for toolsets that can provide server information
type ServerInfoProvider interface {
	GetServerInfo() (serverURL, serverType string)
}
