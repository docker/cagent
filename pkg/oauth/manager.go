package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// manager implements the Manager interface
type manager struct {
	emitAuthRequired         func(serverURL, serverType, status string)
	resumeAuthorizeOauthFlow chan bool
	resumeOauthCodeReceived  chan CallbackResult
	callbackServer           *CallbackServer
	serverMutex              sync.Mutex
	redirectURI              string
	port                     int
	managedServer            bool
}

// NewManager creates a new OAuth manager with optional port configuration
func NewManager(emitAuthRequired func(serverURL, serverType, status string), opts ...ManagerOption) Manager {
	m := &manager{
		emitAuthRequired:         emitAuthRequired,
		resumeAuthorizeOauthFlow: make(chan bool),
		resumeOauthCodeReceived:  make(chan CallbackResult),
		port:                     8083,
		managedServer:            true,
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Set redirect URI based on port
	if m.redirectURI == "" {
		m.redirectURI = fmt.Sprintf("http://localhost:%d/oauth-callback", m.port)
	}

	return m
}

// ManagerOption configures the OAuth manager
type ManagerOption func(*manager)

// WithPort sets the callback server port
func WithPort(port int) ManagerOption {
	return func(m *manager) {
		m.port = port
	}
}

// WithRedirectURI sets a custom redirect URI
func WithRedirectURI(uri string) ManagerOption {
	return func(m *manager) {
		m.redirectURI = uri
	}
}

func WithManagedServer(managed bool) ManagerOption {
	return func(m *manager) {
		m.managedServer = managed
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
func (m *manager) StartAuthorizationFlow(ctx context.Context, confirmation bool) {
	slog.Debug("Receiving OAuth authorization start signal", "confirmation", confirmation)

	select {
	case <-ctx.Done():
		slog.Debug("Context cancelled while sending OAuth start signal")
	case m.resumeAuthorizeOauthFlow <- confirmation:
		slog.Debug("Starting OAuth authorization signal sent", "confirmation", confirmation)
	default:
		slog.Debug("Starting OAuth authorization channel not ready, ignoring")
	}
}

// SendAuthorizationCode sends the OAuth authorization code and state after user has completed the OAuth flow
func (m *manager) SendAuthorizationCode(ctx context.Context, code, state string) error {
	slog.Debug("Sending OAuth authorization code and state")
	result := CallbackResult{
		Code:  code,
		State: state,
	}
	select {
	case <-ctx.Done():
		slog.Debug("Context cancelled while sending OAuth code")
		return ctx.Err()
	case m.resumeOauthCodeReceived <- result:
		slog.Debug("OAuth authorization code and state sent successfully")
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
	authURL, err := oauthHandler.GetAuthorizationURL(ctx, state, codeChallenge)
	if err != nil {
		return fmt.Errorf("failed to get authorization URL: %w", err)
	}

	// Open the browser to the authorization URL
	slog.Info("Opening browser for OAuth authorization", "url", authURL)
	err = OpenBrowser(ctx, authURL)
	if err != nil {
		slog.Warn("Failed to open browser automatically", "error", err, "url", authURL)
	}

	// Wait for the authorization code to be received
	slog.Debug("Waiting for OAuth authorization code")
	var code string

	// Ensure callback server is started if needed
	if err := m.ensureCallbackServer(ctx); err != nil {
		slog.Warn("Failed to start callback server, falling back to manual input", "error", err)
	}

	if m.managedServer {
		// Check if we have a callback server running (either global or our own)
		if callbackServer := m.getCallbackServer(); callbackServer != nil {
			slog.Debug("Using callback server for OAuth authorization")
			// Wait for callback from the browser
			callbackCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			result, err := callbackServer.WaitForCallback(callbackCtx)
			if err != nil {
				if err == context.DeadlineExceeded {
					return fmt.Errorf("OAuth authorization timed out after 5 minutes")
				}
				return fmt.Errorf("failed to wait for OAuth callback: %w", err)
			}

			if result.Error != "" {
				return fmt.Errorf("OAuth authorization error: %s", result.Error)
			}

			if result.Code == "" {
				return fmt.Errorf("no authorization code received from OAuth callback")
			}

			// Verify state parameter matches
			receivedState := result.State
			if receivedState != state {
				slog.Warn("OAuth state mismatch", "expected", state, "received", receivedState)
			}

			code = result.Code
			slog.Debug("Received OAuth code via callback server", "code_present", code != "")
		} else {
			return fmt.Errorf("no callback server available for OAuth authorization")
		}
	} else {
		// Fallback to manual input
		slog.Debug("No callback server available, waiting for manual input")
		var result CallbackResult
		select {
		case result = <-m.resumeOauthCodeReceived:
			slog.Debug("Received OAuth code and state via manual input", "code_present", result.Code != "", "state_present", result.State != "")

			// Validate state parameter matches
			if result.State != state {
				slog.Error("OAuth state mismatch", "expected", state, "received", result.State)
				return fmt.Errorf("OAuth state mismatch: possible CSRF attack")
			}

			code = result.Code
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Exchange the authorization code for a token using the same state and codeVerifier
	slog.Debug("Exchanging authorization code for token")
	err = oauthHandler.ProcessAuthorizationResponse(ctx, code, state, codeVerifier)
	if err != nil {
		return fmt.Errorf("failed to process authorization response: %w", err)
	}

	slog.Info("OAuth authorization completed successfully")
	return nil
}

// ensureCallbackServer starts the callback server if it's not already running
func (m *manager) ensureCallbackServer(ctx context.Context) error {
	m.serverMutex.Lock()
	defer m.serverMutex.Unlock()

	// Check if there's already a global callback server
	if globalServer := GetGlobalCallbackServer(); globalServer != nil {
		slog.Debug("Using existing global callback server")
		return nil
	}

	// Check if we already have our own server
	if m.callbackServer != nil {
		slog.Debug("Callback server already started")
		return nil
	}

	// Create and start new callback server
	slog.Debug("Starting OAuth callback server on demand", "port", m.port)
	m.callbackServer = NewCallbackServer(m.port)
	if err := m.callbackServer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start OAuth callback server: %w", err)
	}

	// Set as global server so other components can use it
	SetGlobalCallbackServer(m.callbackServer)

	slog.Debug("OAuth callback server started successfully", "port", m.port)
	return nil
}

// getCallbackServer returns the active callback server (either global or local)
func (m *manager) getCallbackServer() *CallbackServer {
	// Prefer global server first
	if globalServer := GetGlobalCallbackServer(); globalServer != nil {
		return globalServer
	}

	// Fall back to our local server
	return m.callbackServer
}

// Cleanup stops the callback server if we own it
func (m *manager) Cleanup(ctx context.Context) error {
	m.serverMutex.Lock()
	defer m.serverMutex.Unlock()

	if m.callbackServer != nil {
		slog.Debug("Stopping OAuth callback server")
		if err := m.callbackServer.Stop(ctx); err != nil {
			slog.Error("Failed to stop OAuth callback server", "error", err)
			return err
		}
		m.callbackServer = nil
	}

	return nil
}
