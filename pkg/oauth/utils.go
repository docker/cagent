package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
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

// ValidateAndSanitizeURL validates and sanitizes a URL for safe browser opening.
//
// This function implements security measures to prevent command injection and malicious URL schemes
// based on security best practices outlined in the Veria Labs article on MCP to shell vulnerabilities.
//
// Security validations:
// - Only allows http:// and https:// schemes (blocks javascript:, data:, file:, etc.)
// - Validates URL format using Go's net/url package
// - Prevents command injection by ensuring URL can be safely passed to system commands
//
// Returns the original URL if valid, or an error if the URL fails security validation.
func ValidateAndSanitizeURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}

	// Parse the URL to validate its format
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		slog.Warn("Invalid URL format blocked", "url", rawURL, "error", err)
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	// Validate scheme - only allow http and https
	scheme := strings.ToLower(parsedURL.Scheme)
	allowedSchemes := map[string]bool{
		"http":  true,
		"https": true,
	}

	if !allowedSchemes[scheme] {
		slog.Warn("Dangerous URL scheme blocked", "url", rawURL, "scheme", scheme)
		return "", fmt.Errorf("URL scheme '%s' is not allowed. Only http:// and https:// are permitted", scheme)
	}

	// Ensure we have a valid host
	if parsedURL.Host == "" {
		slog.Warn("URL with empty host blocked", "url", rawURL)
		return "", fmt.Errorf("URL must have a valid host")
	}

	// Additional safety checks for potential command injection
	// We need to be careful here - some characters like & are legitimate in URLs (query parameters)
	// but dangerous in shell contexts. We'll check for dangerous patterns rather than individual chars.

	// Check for backticks (command substitution)
	if strings.Contains(rawURL, "`") {
		slog.Warn("URL with backticks blocked", "url", rawURL)
		return "", fmt.Errorf("URL contains potentially dangerous character: `")
	}

	// Check for shell command substitution patterns
	if strings.Contains(rawURL, "$(") {
		slog.Warn("URL with command substitution pattern blocked", "url", rawURL)
		return "", fmt.Errorf("URL contains potentially dangerous character: $")
	}

	// Check for command chaining outside of query parameters
	// Allow & in query strings but block it in host/path contexts where it might be dangerous
	urlParts := strings.SplitN(rawURL, "?", 2) // Split on first ? to separate base URL from query
	baseURL := urlParts[0]

	dangerousInBase := []string{";", "|", "<", ">"}
	for _, char := range dangerousInBase {
		if strings.Contains(baseURL, char) {
			slog.Warn("URL with dangerous character in base URL blocked", "url", rawURL, "char", char)
			return "", fmt.Errorf("URL contains potentially dangerous character: %s", char)
		}
	}

	// Check for other dangerous patterns
	dangerousPatterns := []string{
		" && ", " || ", // Command chaining with spaces
		" ; ", // Command separation with spaces
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(rawURL, pattern) {
			slog.Warn("URL with dangerous pattern blocked", "url", rawURL, "pattern", pattern)
			return "", fmt.Errorf("URL contains potentially dangerous pattern: %s", strings.TrimSpace(pattern))
		}
	}

	slog.Debug("URL validated successfully", "url", rawURL)
	return rawURL, nil
}

// OpenBrowser opens the default browser to the specified URL with security validation.
//
// This function now includes security measures to prevent command injection and malicious URL schemes.
// All URLs are validated before being passed to system commands to ensure they are safe to open.
//
// Security features:
// - URL validation and sanitization using ValidateAndSanitizeURL
// - Logging of security events (blocked URLs, successful opens)
// - Maintains backward compatibility for legitimate OAuth URLs
func OpenBrowser(ctx context.Context, urlToOpen string) error {
	// Validate and sanitize the URL before opening
	validatedURL, err := ValidateAndSanitizeURL(urlToOpen)
	if err != nil {
		slog.Error("Blocked attempt to open unsafe URL", "url", urlToOpen, "error", err)
		return fmt.Errorf("URL validation failed: %w", err)
	}

	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", validatedURL}
	case "darwin":
		cmd = "open"
		args = []string{validatedURL}
	case "linux":
		cmd = "xdg-open"
		args = []string{validatedURL}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	slog.Info("Opening browser with validated URL", "url", validatedURL, "platform", runtime.GOOS)

	err = exec.CommandContext(ctx, cmd, args...).Start()
	if err != nil {
		slog.Error("Failed to execute browser command", "cmd", cmd, "args", args, "error", err)
		return fmt.Errorf("failed to open browser: %w", err)
	}

	return nil
}
