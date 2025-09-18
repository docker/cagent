package oauth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// manager implements the Manager interface
type manager struct {
	emitAuthRequired         func(serverURL, serverType, status string)
	resumeAuthorizeOauthFlow chan bool
	resumeOauthCodeReceived  chan string
}

// NewManager creates a new OAuth manager
func NewManager(emitAuthRequired func(serverURL, serverType, status string)) Manager {
	return &manager{
		emitAuthRequired:         emitAuthRequired,
		resumeAuthorizeOauthFlow: make(chan bool),
		resumeOauthCodeReceived:  make(chan string),
	}
}

// HandleAuthorizationFlow handles a single OAuth authorization flow
func (m *manager) HandleAuthorizationFlow(ctx context.Context, sessionID string, oauthErr *AuthorizationRequiredError) error {
	m.emitAuthRequired(oauthErr.ServerURL, oauthErr.ServerType, "pending")

	slog.Debug("Waiting for OAuth authorization to start", "server", oauthErr.ServerURL, "type", oauthErr.ServerType)
	confirmation := <-m.resumeAuthorizeOauthFlow

	if !confirmation {
		slog.Debug("OAuth authorization not confirmed by user, stopping", "server", oauthErr.ServerURL)
		m.emitAuthRequired(oauthErr.ServerURL, oauthErr.ServerType, "denied")
		return fmt.Errorf("OAuth authorization denied by user")
	}

	// Start the OAuth authorization flow
	slog.Debug("Starting OAuth authorization flow", "server", oauthErr.ServerURL, "type", oauthErr.ServerType)
	authCompleted := make(chan error, 1)
	// Extract the OAuth handler from the original error
	oauthHandler := client.GetOAuthHandler(oauthErr.Err)
	go func() {
		authCompleted <- m.performOAuthAuthorization(ctx, sessionID, oauthHandler)
	}()

	select {
	case authErr := <-authCompleted:
		if authErr != nil {
			slog.Error("OAuth authorization failed", "server", oauthErr.ServerURL, "error", authErr)
			return fmt.Errorf("OAuth authorization failed: %v", authErr)
		}
		slog.Debug("OAuth authorization completed", "server", oauthErr.ServerURL)
		m.emitAuthRequired(oauthErr.ServerURL, oauthErr.ServerType, "confirmed")
		return nil
	case <-ctx.Done():
		slog.Debug("Context cancelled while waiting for OAuth authorization", "server", oauthErr.ServerURL)
		return ctx.Err()
	}
}

// StartAuthorizationFlow signals that user confirmation has been given to start the OAuth flow
func (m *manager) StartAuthorizationFlow(confirmation bool) {
	slog.Debug("Receiving OAuth authorization start signal", "confirmation", confirmation)

	select {
	case m.resumeAuthorizeOauthFlow <- confirmation:
		slog.Debug("Starting OAuth authorization signal sent", "confirmation", confirmation)
	default:
		slog.Debug("Starting OAuth authorization channel not ready, ignoring")
	}
}

// SendAuthorizationCode sends the OAuth authorization code after user has completed the OAuth flow
func (m *manager) SendAuthorizationCode(code string) error {
	slog.Debug("Sending OAuth authorization code")
	select {
	case m.resumeOauthCodeReceived <- code:
		slog.Debug("OAuth authorization code sent successfully")
		return nil
	default:
		slog.Debug("OAuth code channel not ready")
		return fmt.Errorf("OAuth flow not in progress")
	}
}

// performOAuthAuthorization performs the OAuth authorization flow
func (m *manager) performOAuthAuthorization(ctx context.Context, sessionID string, oauthHandler *transport.OAuthHandler) error {
	slog.Debug("Starting OAuth authorization flow")

	// Generate PKCE code verifier and challenge
	codeVerifier, err := client.GenerateCodeVerifier()
	if err != nil {
		return fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := client.GenerateCodeChallenge(codeVerifier)

	// Generate state parameter with encoded session ID
	state, err := GenerateStateWithSessionID(sessionID)
	if err != nil {
		return fmt.Errorf("failed to generate state with session ID: %w", err)
	}

	// Register client if no client ID is available
	if oauthHandler.GetClientID() == "" {
		slog.Debug("Registering OAuth client")
		err = oauthHandler.RegisterClient(ctx, "cagent-oauth-client")
		if err != nil {
			return fmt.Errorf("failed to register client: %w", err)
		}
	}

	// Get the authorization URL using our corrected implementation
	authURL, err := GetAuthorizationURL(ctx, oauthHandler, state, codeChallenge)
	if err != nil {
		return fmt.Errorf("failed to get authorization URL: %w", err)
	}

	// Open the browser to the authorization URL
	slog.Info("Opening browser for OAuth authorization", "url", authURL)
	err = OpenBrowser(authURL)
	if err != nil {
		slog.Warn("Failed to open browser automatically", "error", err, "url", authURL)
	}

	// Wait for the authorization code to be received
	slog.Debug("Waiting for OAuth authorization code")
	code := <-m.resumeOauthCodeReceived

	// Exchange the authorization code for a token using the same state and codeVerifier
	slog.Debug("Exchanging authorization code for token")
	err = oauthHandler.ProcessAuthorizationResponse(ctx, code, state, codeVerifier)
	if err != nil {
		return fmt.Errorf("failed to process authorization response: %w", err)
	}

	slog.Info("OAuth authorization completed successfully")
	return nil
}
