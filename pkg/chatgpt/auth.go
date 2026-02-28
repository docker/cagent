// Package chatgpt implements OAuth authentication for ChatGPT Plus/Pro subscriptions.
// It uses the OAuth2 PKCE flow against auth.openai.com to obtain access tokens
// that can be exchanged for an OpenAI API key.
package chatgpt

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"

	"github.com/docker/cagent/pkg/browser"
)

const (
	// OAuth endpoints for ChatGPT authentication
	authorizationEndpoint = "https://auth.openai.com/oauth/authorize"

	// OAuth client configuration (same as Codex CLI)
	clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// OAuth scopes
	defaultScopes = "openid profile email offline_access"

	// defaultPort is the preferred local port for the OAuth callback server.
	defaultPort = 1455
)

// tokenEndpointURL is the OAuth token endpoint. It is a variable so tests can override it.
var tokenEndpointURL = "https://auth.openai.com/oauth/token"

// Token represents the persisted authentication state from the ChatGPT OAuth flow.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsExpired checks if the token is expired.
// Returns true if the token will expire within 60 seconds.
func (t *Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(60 * time.Second).After(t.ExpiresAt)
}

// Login performs the OAuth PKCE flow to authenticate with ChatGPT.
// It opens the user's browser to the OpenAI login page, starts a local
// callback server, exchanges the authorization code for tokens, and then
// exchanges the id_token for a standard OpenAI API key.
func Login(ctx context.Context) (*Token, error) {
	// Generate PKCE code verifier and challenge
	verifier := oauth2.GenerateVerifier()
	challenge := oauth2.S256ChallengeFromVerifier(verifier)

	// Generate random state for CSRF protection
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Start local callback server, preferring the default port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		// Fall back to a random port
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("failed to start callback server: %w", err)
		}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", port)

	// Build authorization URL
	authURL := buildAuthURL(redirectURI, state, challenge)

	slog.Debug("Starting ChatGPT OAuth login", "redirect_uri", redirectURI)

	// Channel to receive the authorization code
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	// Set up callback handler
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Verify state
		if r.URL.Query().Get("state") != state {
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch")}
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		// Check for errors
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			resultCh <- callbackResult{err: fmt.Errorf("OAuth error: %s: %s", errParam, desc)}
			fmt.Fprintf(w, "<html><body><h1>Authentication failed</h1><p>%s</p><p>You can close this window.</p></body></html>", desc)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			resultCh <- callbackResult{err: fmt.Errorf("no authorization code received")}
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		resultCh <- callbackResult{code: code}
		fmt.Fprint(w, "<html><body><h1>Authentication successful!</h1><p>You can close this window and return to the terminal.</p></body></html>")
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in background
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("Callback server error", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	// Open browser
	if err := browser.Open(ctx, authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser (visit this URL manually):\n%s\n\nerror: %w", authURL, err)
	}

	// Wait for callback or context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("authentication failed: %w", result.err)
		}

		// Exchange code for OAuth tokens
		tokens, err := exchangeCode(ctx, result.code, verifier, redirectURI)
		if err != nil {
			return nil, err
		}

		// Exchange id_token for an OpenAI API key
		apiKey, err := exchangeForAPIKey(ctx, tokens.IDToken)
		if err != nil {
			return nil, fmt.Errorf("failed to obtain API key: %w", err)
		}

		tokens.AccessToken = apiKey
		return tokens, nil
	}
}

// RefreshAccessToken refreshes an expired access token using the refresh token.
// It obtains new OAuth tokens and then exchanges the new id_token for an API key.
func RefreshAccessToken(ctx context.Context, refreshToken string) (*Token, error) {
	payload, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refreshToken,
		"scope":         "openid profile email",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	var refreshResp struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := postJSON(ctx, tokenEndpointURL, "application/json", bytes.NewReader(payload), &refreshResp); err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	// Use new refresh token if provided, otherwise keep the old one
	newRefreshToken := refreshResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	// Exchange the new id_token for an API key
	if refreshResp.IDToken == "" {
		return nil, fmt.Errorf("refresh response did not include an id_token")
	}

	apiKey, err := exchangeForAPIKey(ctx, refreshResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain API key after refresh: %w", err)
	}

	slog.Debug("ChatGPT token refreshed successfully")
	return &Token{
		AccessToken:  apiKey,
		RefreshToken: newRefreshToken,
		IDToken:      refreshResp.IDToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}, nil
}

// buildAuthURL constructs the OAuth authorization URL with PKCE parameters.
func buildAuthURL(redirectURI, state, codeChallenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", defaultScopes)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	return authorizationEndpoint + "?" + params.Encode()
}

// exchangeCode exchanges an authorization code for OAuth tokens (id_token, access_token, refresh_token).
func exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", clientID)
	data.Set("code_verifier", verifier)

	var tokenResp struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := postForm(ctx, tokenEndpointURL, data, &tokenResp); err != nil {
		return nil, fmt.Errorf("code exchange failed: %w", err)
	}

	slog.Debug("ChatGPT OAuth code exchange successful")
	return &Token{
		IDToken:      tokenResp.IDToken,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}, nil
}

// exchangeForAPIKey exchanges an id_token for a standard OpenAI API key
// using the token exchange grant type.
func exchangeForAPIKey(ctx context.Context, idToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("client_id", clientID)
	data.Set("requested_token", "openai-api-key")
	data.Set("subject_token", idToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:id_token")

	var exchangeResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := postForm(ctx, tokenEndpointURL, data, &exchangeResp); err != nil {
		return "", fmt.Errorf("API key exchange failed: %w", err)
	}

	return exchangeResp.AccessToken, nil
}

// postForm sends a POST request with form-encoded data and decodes the JSON response.
func postForm(ctx context.Context, endpoint string, data url.Values, result any) error {
	return postJSON(ctx, endpoint, "application/x-www-form-urlencoded", bytes.NewBufferString(data.Encode()), result)
}

// postJSON sends a POST request with the given content type and body, then
// decodes the JSON response into result.
func postJSON(ctx context.Context, endpoint, contentType string, body io.Reader, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// generateState generates a random state string for CSRF protection.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
