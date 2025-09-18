package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
)

// StateData represents the data encoded in the OAuth state parameter.
//
// In OAuth flows, the state parameter serves dual purposes:
//  1. Security: CSRF protection by including random data
//  2. Session tracking: When the browser returns from authorization, we need to know
//     which session triggered the OAuth flow to route the callback correctly.
//
// Since OAuth authorization happens in a browser (different context from our runtime),
// we embed the session ID in the state parameter so we can retrieve it when the
// authorization server redirects back to us with the authorization code.
type StateData struct {
	SessionID string `json:"session_id"` // The session ID that initiated the OAuth flow
	Random    string `json:"random"`     // Random component for CSRF protection
}

// GenerateStateWithSessionID generates an OAuth state parameter that encodes the session ID.
//
// OAuth State Parameter Design:
//
// When an agent needs OAuth authorization, the flow works like this:
// 1. Agent runtime detects OAuth is needed and pauses execution
// 2. We generate authorization URL with state parameter containing the session ID
// 3. User's browser is redirected to the OAuth provider for authorization
// 4. OAuth provider redirects back to our callback URL with the authorization code AND the state
// 5. Our callback handler receives the state, extracts the session ID from it
// 6. We can then resume the correct agent session with the authorization code
//
// Without encoding session ID in state, we couldn't match the OAuth callback to the
// specific agent session that requested authorization, especially in multi-session scenarios.
//
// The state parameter combines:
// - Session ID: To route the callback back to the correct session
// - Random bytes: For CSRF protection (traditional OAuth security requirement)
func GenerateStateWithSessionID(sessionID string) (string, error) {
	// Generate a random component for security
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	stateData := StateData{
		SessionID: sessionID,
		Random:    base64.RawURLEncoding.EncodeToString(randomBytes),
	}

	// JSON encode the state data
	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal state data: %w", err)
	}

	// Base64 encode the JSON to create the state parameter
	state := base64.RawURLEncoding.EncodeToString(stateJSON)
	return state, nil
}

// DecodeSessionIDFromState extracts the session ID from an OAuth state parameter.
//
// This function is used by OAuth callback handlers to decode session IDs.
// When the OAuth provider redirects back to our callback endpoint, they include the
// state parameter we originally sent. This function reverses the encoding done by
// GenerateStateWithSessionID to extract the session ID, allowing us to route the
// authorization code back to the correct agent session that initiated the OAuth flow.
//
// This is the critical piece that bridges the browser-based OAuth callback back to
// the specific runtime session that needs the authorization.
func DecodeSessionIDFromState(state string) (string, error) {
	// Base64 decode the state
	stateJSON, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", fmt.Errorf("failed to decode state: %w", err)
	}

	// JSON decode the state data
	var stateData StateData
	if err := json.Unmarshal(stateJSON, &stateData); err != nil {
		return "", fmt.Errorf("failed to unmarshal state data: %w", err)
	}

	return stateData.SessionID, nil
}

// OpenBrowser opens the default browser to the specified URL
func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return exec.Command(cmd, args...).Start()
}
