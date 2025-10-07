package mcp

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// newRemoteClient creates a new MCP client that can connect to a remote MCP server
func newRemoteClient(url, transportType string, headers map[string]string, redirectURI string, tokenStore client.TokenStore) (*client.Client, error) {
	slog.Debug("Creating remote MCP client", "url", url, "transport", transportType, "headers", headers, "redirectURI", redirectURI)

	// Detect if the server requires OAuth authentication
	requiresOAuth := detectOAuthRequirement(url)

	oauthConfig := client.OAuthConfig{
		RedirectURI: redirectURI,
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	if transportType == "sse" {
		options := []transport.ClientOption{transport.WithHeaders(headers)}
		if requiresOAuth {
			options = append(options, transport.WithOAuth(oauthConfig))
		}

		c, err := client.NewSSEMCPClient(url, options...)
		if err != nil {
			return nil, fmt.Errorf("failed to create MCP client: %w", err)
		}

		slog.Debug("Created sse remote MCP client successfully", "url", url, "transport", transportType, "requiresOAuth", requiresOAuth)
		return c, nil
	}

	options := []transport.StreamableHTTPCOption{transport.WithHTTPHeaders(headers)}
	if requiresOAuth {
		options = append(options, transport.WithHTTPOAuth(oauthConfig))
	}

	c, err := client.NewStreamableHttpClient(url, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	slog.Debug("Created streamable remote MCP client successfully", "url", url, "transport", transportType, "requiresOAuth", requiresOAuth)
	return c, nil
}

// detectOAuthRequirement checks if the server requires OAuth authentication
// by making test requests and checking for WWW-Authenticate header.
// It tries GET first, then POST if GET returns 405 Method Not Allowed.
// See https://modelcontextprotocol.io/specification/draft/basic/authorization#authorization-server-location.
func detectOAuthRequirement(url string) bool {
	httpClient := &http.Client{}

	// Try GET request first
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		slog.Debug("Failed to create GET test request for OAuth detection", "error", err)
		return false
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug("Failed to make GET test request for OAuth detection", "error", err)
		return false
	}
	defer resp.Body.Close()

	// Check for WWW-Authenticate header in GET response
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth != "" {
		slog.Debug("Detected OAuth requirement via GET", "www-authenticate", wwwAuth)
		return strings.Contains(strings.ToLower(wwwAuth), "bearer") ||
			strings.Contains(strings.ToLower(wwwAuth), "oauth")
	}

	// If GET returned 405 Method Not Allowed, try POST
	if resp.StatusCode == http.StatusMethodNotAllowed {
		slog.Debug("GET returned 405, trying POST for OAuth detection")

		postReq, err := http.NewRequest(http.MethodPost, url, http.NoBody)
		if err != nil {
			slog.Debug("Failed to create POST test request for OAuth detection", "error", err)
			return false
		}

		postResp, err := httpClient.Do(postReq)
		if err != nil {
			slog.Debug("Failed to make POST test request for OAuth detection", "error", err)
			return false
		}
		defer postResp.Body.Close()

		// Check for WWW-Authenticate header in POST response
		postWwwAuth := postResp.Header.Get("WWW-Authenticate")
		if postWwwAuth != "" {
			slog.Debug("Detected OAuth requirement via POST", "www-authenticate", postWwwAuth)
			return strings.Contains(strings.ToLower(postWwwAuth), "bearer") ||
				strings.Contains(strings.ToLower(postWwwAuth), "oauth")
		}
	}

	return false
}
