// Package oauth provides OAuth discovery and authorization URL handling for MCP servers.
//
// OAuth Discovery Implementation Gap in MCP Ecosystem:
//
// While the MCP Specification technically describes using /.well-known/oauth-protected-resource
// for discovery (RFC 9728, see https://www.speakeasy.com/mcp/building-servers/state-of-oauth-in-mcp),
// most current MCP servers actually look for
// /.well-known/oauth-authorization-server directly at the MCP server's domain (RFC 8414).
// This is how the OAuth dance works in practice today. The gap between specification and
// implementation is prevalent in the MCP ecosystem.
//
// The mcp-go oauthHandler.GetAuthorizationURL doesn't handle this correctly and follows only
// the strict RFC 9728 path, which fails with most real-world MCP servers. While waiting for
// this PR https://github.com/mark3labs/mcp-go/pull/581 to fix the upstream discovery logic,
// we need a workaround that implements the correct discovery sequence that actually works
// with existing MCP servers.
//
// This implementation bridges the gap by:
//  1. First trying the RFC 9728 approach (/.well-known/oauth-protected-resource)
//  2. Falling back to the RFC 8414 approach (/.well-known/oauth-authorization-server)
//     which is what most MCP servers actually implement
//  3. Providing sensible defaults as a final fallback
//
// This ensures compatibility with both specification-compliant servers and the majority
// of existing MCP server implementations that follow the more common OAuth patterns.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// AuthServerMetadata represents the OAuth 2.0 Authorization Server Metadata
// This is a copy from the mcp-go library to avoid importing internal types
type AuthServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	JwksURI                           string   `json:"jwks_uri,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// ProtectedResource represents the response from /.well-known/oauth-protected-resource
type ProtectedResource struct {
	AuthorizationServers []string `json:"authorization_servers"`
	Resource             string   `json:"resource"`
	ResourceName         string   `json:"resource_name,omitempty"`
}

// GetAuthorizationURL gets the OAuth authorization URL using the correct discovery logic.
//
// This function works around the mcp-go library's incomplete OAuth discovery implementation.
// The upstream GetAuthorizationURL method fails with most MCP servers because it only tries
// the RFC 9728 discovery path, while most servers implement RFC 8414 directly.
//
// Our approach:
// 1. Use our custom discovery logic to find the correct authorization endpoint
// 2. Leverage the existing mcp-go logic for parameter construction (client_id, scope, etc.)
// 3. Combine the correct endpoint with the correct parameters
//
// This ensures we get properly formatted OAuth URLs that work with real MCP servers.
func GetAuthorizationURL(ctx context.Context, oauthHandler *transport.OAuthHandler, state, codeChallenge string) (string, error) {
	// Get server metadata using our corrected discovery logic that handles the spec vs reality gap
	metadata, err := getCorrectServerMetadata(ctx, oauthHandler)
	if err != nil {
		return "", fmt.Errorf("failed to get server metadata: %w", err)
	}

	// Store the state for later validation
	oauthHandler.SetExpectedState(state)

	// Use the handler's existing logic to build the URL parameters correctly
	// but replace the base URL with our corrected authorization endpoint
	originalURL, err := oauthHandler.GetAuthorizationURL(ctx, state, codeChallenge)
	if err != nil {
		return "", fmt.Errorf("failed to get authorization URL from handler: %w", err)
	}

	// Parse the original URL to extract parameters
	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse authorization URL: %w", err)
	}

	// Replace the base URL with our corrected authorization endpoint
	return metadata.AuthorizationEndpoint + "?" + parsedURL.RawQuery, nil
}

// getCorrectServerMetadata implements the OAuth discovery sequence that works with real MCP servers.
//
// The MCP specification suggests using RFC 9728 (/.well-known/oauth-protected-resource) for
// discovery, but in practice most MCP servers implement RFC 8414 (/.well-known/oauth-authorization-server)
// directly. This creates a gap where spec-compliant clients fail with most real implementations.
//
// Discovery sequence (in order of preference):
// 1. Try /.well-known/oauth-protected-resource (RFC 9728) - for spec-compliant servers
// 2. Try /.well-known/oauth-authorization-server (RFC 8414) - for most real MCP servers
// 3. Try /.well-known/openid-configuration (OpenID Connect) - for OIDC-based implementations
// 4. Use default endpoints - final fallback for servers with minimal OAuth support
//
// This pragmatic approach ensures compatibility with the widest range of MCP server implementations
// while maintaining compliance with the specification where possible.
func getCorrectServerMetadata(ctx context.Context, oauthHandler *transport.OAuthHandler) (*AuthServerMetadata, error) {
	// Extract base URL from the OAuth handler
	// We'll use the existing metadata call just to get the base URL, then do our own discovery
	baseURL, err := extractBaseURLFromHandler(ctx, oauthHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base URL: %w", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Try OAuth Protected Resource discovery (RFC 9728)
	protectedResourceURL := baseURL + "/.well-known/oauth-protected-resource"
	metadata, err := tryProtectedResourceDiscovery(ctx, httpClient, protectedResourceURL)
	if err == nil {
		return metadata, nil
	}

	// Step 2: Try OAuth Authorization Server Metadata discovery (RFC 8414)
	authServerURL := baseURL + "/.well-known/oauth-authorization-server"
	metadata, err = tryDirectMetadataDiscovery(ctx, httpClient, authServerURL)
	if err == nil {
		return metadata, nil
	}

	// Step 3: Try OpenID Connect discovery as additional fallback
	openidURL := baseURL + "/.well-known/openid-configuration"
	metadata, err = tryDirectMetadataDiscovery(ctx, httpClient, openidURL)
	if err == nil {
		return metadata, nil
	}

	// Step 4: Use default endpoints as final fallback
	return getDefaultEndpoints(baseURL), nil
}

// extractBaseURLFromHandler extracts the base URL from the OAuth handler.
//
// This is a necessary workaround because the mcp-go OAuth handler doesn't expose
// the base URL directly. We call the existing (flawed) GetServerMetadata method
// not for its discovery logic, but to extract the base URL that we can then use
// for our own corrected discovery sequence.
//
// The irony is that we're calling the broken method to get the URL, then ignoring
// its discovery results and doing our own discovery that actually works.
func extractBaseURLFromHandler(ctx context.Context, oauthHandler *transport.OAuthHandler) (string, error) {
	// We need to get the base URL somehow. The cleanest way is to call the existing
	// metadata method and extract the base URL from the result, even though the
	// discovery logic in that method is flawed.
	existingMetadata, err := oauthHandler.GetServerMetadata(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get base URL from handler: %w", err)
	}

	// Extract base URL from any endpoint (authorization endpoint is always available)
	parsedURL, err := url.Parse(existingMetadata.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse authorization endpoint: %w", err)
	}

	// Build base URL without path
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
}

// tryProtectedResourceDiscovery tries OAuth Protected Resource discovery (RFC 9728)
func tryProtectedResourceDiscovery(ctx context.Context, httpClient *http.Client, protectedResourceURL string) (*AuthServerMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, protectedResourceURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create protected resource request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send protected resource request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("protected resource discovery failed with status %d", resp.StatusCode)
	}

	var protectedResource ProtectedResource
	if err := json.NewDecoder(resp.Body).Decode(&protectedResource); err != nil {
		return nil, fmt.Errorf("failed to decode protected resource response: %w", err)
	}

	// If no authorization servers are specified, this discovery method failed
	if len(protectedResource.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("no authorization servers found in protected resource")
	}

	// Use the first authorization server and try to get its metadata
	authServerURL := protectedResource.AuthorizationServers[0]

	// Try OpenID Connect discovery first
	metadata, err := tryDirectMetadataDiscovery(ctx, httpClient, authServerURL+"/.well-known/openid-configuration")
	if err == nil {
		return metadata, nil
	}

	// If OpenID Connect discovery fails, try OAuth Authorization Server Metadata
	return tryDirectMetadataDiscovery(ctx, httpClient, authServerURL+"/.well-known/oauth-authorization-server")
}

// tryDirectMetadataDiscovery tries to fetch metadata directly from a discovery URL
func tryDirectMetadataDiscovery(ctx context.Context, httpClient *http.Client, metadataURL string) (*AuthServerMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send metadata request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata discovery failed with status %d", resp.StatusCode)
	}

	var metadata AuthServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata response: %w", err)
	}

	return &metadata, nil
}

// getDefaultEndpoints returns default OAuth endpoints based on the base URL
func getDefaultEndpoints(baseURL string) *AuthServerMetadata {
	// Parse the base URL to extract the authority
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		// If parsing fails, just use the baseURL as-is
		return &AuthServerMetadata{
			Issuer:                baseURL,
			AuthorizationEndpoint: baseURL + "/authorize",
			TokenEndpoint:         baseURL + "/token",
			RegistrationEndpoint:  baseURL + "/register",
		}
	}

	// Discard any path component to get the authorization base URL
	parsedURL.Path = ""
	authBaseURL := parsedURL.String()

	// Remove trailing slash if present
	authBaseURL = strings.TrimSuffix(authBaseURL, "/")

	return &AuthServerMetadata{
		Issuer:                authBaseURL,
		AuthorizationEndpoint: authBaseURL + "/authorize",
		TokenEndpoint:         authBaseURL + "/token",
		RegistrationEndpoint:  authBaseURL + "/register",
	}
}
