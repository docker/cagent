package mcp

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// detectOAuthRequirement checks if the server requires OAuth authentication
// by making a test request and checking for WWW-Authenticate header.
// See https://modelcontextprotocol.io/specification/draft/basic/authorization#authorization-server-location.
func detectOAuthRequirement(url string) bool {
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		slog.Debug("Failed to create test request for OAuth detection", "error", err)
		return false
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug("Failed to make test request for OAuth detection", "error", err)
		return false
	}
	defer resp.Body.Close()

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth != "" {
		slog.Debug("Detected OAuth requirement", "www-authenticate", wwwAuth)
		return strings.Contains(strings.ToLower(wwwAuth), "bearer") ||
			strings.Contains(strings.ToLower(wwwAuth), "oauth")
	}

	return false
}

// NewRemoteClient creates a new MCP client that can connect to a remote MCP server
func NewRemoteClient(url, transportType string, headers map[string]string, redirectURI string) (*Client, error) {
	slog.Debug("Creating remote MCP client", "url", url, "transport", transportType, "headers", headers, "redirectURI", redirectURI)

	// Detect if the server requires OAuth authentication
	requiresOAuth := detectOAuthRequirement(url)

	var c *client.Client
	var err error

	if requiresOAuth {
		tokenStore := client.NewMemoryTokenStore()

		oauthConfig := client.OAuthConfig{
			RedirectURI: redirectURI,
			TokenStore:  tokenStore,
			PKCEEnabled: true,
		}

		if transportType == "sse" {
			c, err = client.NewOAuthSSEClient(url, oauthConfig)
			if err != nil {
				slog.Error("Failed to create OAuth SSE remote MCP client", "error", err)
				return nil, fmt.Errorf("failed to create OAuth SSE remote MCP client: %w", err)
			}
		} else {
			c, err = client.NewOAuthStreamableHttpClient(url, oauthConfig)
			if err != nil {
				slog.Error("Failed to create OAuth streamable remote MCP client", "error", err)
				return nil, fmt.Errorf("failed to create OAuth streamable remote MCP client: %w", err)
			}
		}
	} else {
		if transportType == "sse" {
			c, err = client.NewSSEMCPClient(url, client.WithHeaders(headers))
			if err != nil {
				slog.Error("Failed to create sse remote MCP client", "error", err)
				return nil, fmt.Errorf("failed to create sse remote MCP client: %w", err)
			}
		} else {
			c, err = client.NewStreamableHttpClient(url, transport.WithHTTPHeaders(headers))
			if err != nil {
				slog.Error("Failed to create streamable remote MCP client", "error", err)
				return nil, fmt.Errorf("failed to create streamable remote MCP client: %w", err)
			}
		}
	}

	slog.Debug("Created remote MCP client successfully")
	return &Client{
		client:  c,
		logType: "remote",
		logId:   url,
	}, nil
}
