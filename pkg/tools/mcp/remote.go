package mcp

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/tools"
)

type remoteMCPClient struct {
	session             *mcp.ClientSession
	url                 string
	transportType       string
	headers             map[string]string
	redirectURI         string
	tokenStore          OAuthTokenStore
	elicitationHandler  tools.ElicitationHandler
	oauthSuccessHandler func()
	mu                  sync.RWMutex
}

func newRemoteClient(url, transportType string, headers map[string]string, redirectURI string, tokenStore OAuthTokenStore) *remoteMCPClient {
	slog.Debug("Creating remote MCP client", "url", url, "transport", transportType, "headers", headers, "redirectURI", redirectURI)

	if tokenStore == nil {
		tokenStore = NewInMemoryTokenStore()
	}

	return &remoteMCPClient{
		url:           url,
		transportType: transportType,
		headers:       headers,
		redirectURI:   redirectURI,
		tokenStore:    tokenStore,
	}
}

func (c *remoteMCPClient) oauthSuccess() {
	if c.oauthSuccessHandler != nil {
		c.oauthSuccessHandler()
	}
}

func (c *remoteMCPClient) requestElicitation(ctx context.Context, req *mcp.ElicitParams) (tools.ElicitationResult, error) {
	if c.elicitationHandler == nil {
		return tools.ElicitationResult{}, fmt.Errorf("no elicitation handler configured")
	}

	// Call the handler which should propagate the request to the runtime's client
	result, err := c.elicitationHandler(ctx, req)
	if err != nil {
		return tools.ElicitationResult{}, err
	}

	return result, nil
}

// handleElicitationRequest forwards incoming elicitation requests from the MCP server
func (c *remoteMCPClient) handleElicitationRequest(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	slog.Debug("Received elicitation request from MCP server", "message", req.Params.Message)

	result, err := c.requestElicitation(ctx, req.Params)
	if err != nil {
		return nil, fmt.Errorf("elicitation failed: %w", err)
	}

	return &mcp.ElicitResult{
		Action:  result.Action,
		Content: result.Content,
	}, nil
}

func (c *remoteMCPClient) Start(ctx context.Context) error {
	return nil
}

func (c *remoteMCPClient) Initialize(ctx context.Context, request *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	// Create HTTP client with OAuth support
	httpClient := c.createHTTPClient()

	// Pre-flight request to trigger OAuth flow if needed, before establishing MCP session
	// This prevents OAuth from interrupting the MCP session initialization
	slog.Debug("Performing pre-flight authentication check", "url", c.url)
	if err := c.preflightAuthCheck(ctx, httpClient); err != nil {
		return nil, fmt.Errorf("pre-flight authentication check failed: %w", err)
	}
	slog.Debug("Pre-flight authentication check completed successfully")

	var transport mcp.Transport

	switch c.transportType {
	case "sse":
		transport = &mcp.SSEClientTransport{
			Endpoint:   c.url,
			HTTPClient: httpClient,
		}
	case "streamable", "streamable-http":
		transport = &mcp.StreamableClientTransport{
			Endpoint:   c.url,
			HTTPClient: httpClient,
		}
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", c.transportType)
	}

	// Create an MCP client with elicitation support
	impl := &mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
	}

	opts := &mcp.ClientOptions{
		ElicitationHandler: c.handleElicitationRequest,
	}

	client := mcp.NewClient(impl, opts)

	// Connect to the MCP server (OAuth should already be handled by pre-flight)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	c.mu.Lock()
	c.session = session
	c.mu.Unlock()

	slog.Debug("Remote MCP client connected successfully")
	return session.InitializeResult(), nil
}

// preflightAuthCheck performs a lightweight HTTP request to trigger OAuth flow before MCP session creation
// This prevents OAuth from interrupting the MCP protocol handshake
// Without this, the MCP session initialization may fail if the server responds with 401 Unauthorized.
// Example of remote server that behaves this way: https://mcp.prisma.io/mcp
func (c *remoteMCPClient) preflightAuthCheck(ctx context.Context, httpClient *http.Client) error {
	// Make a HEAD request to the MCP endpoint to trigger OAuth if needed
	// The oauthTransport will intercept any 401 response and complete the OAuth flow
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create pre-flight request: %w", err)
	}

	// Add any configured headers
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pre-flight request failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 2xx and 4xx responses (except 401 which should have triggered OAuth)
	// We just want to ensure OAuth is complete, not validate the endpoint yet
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed: still unauthorized after OAuth attempt")
	}

	slog.Debug("Pre-flight check completed", "status", resp.StatusCode)
	return nil
}

// createHTTPClient creates an HTTP client with OAuth support
func (c *remoteMCPClient) createHTTPClient() *http.Client {
	return &http.Client{
		Transport: &oauthTransport{
			base:       http.DefaultTransport,
			client:     c,
			tokenStore: c.tokenStore,
			baseURL:    c.url,
		},
	}
}

func (c *remoteMCPClient) Close() error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session != nil {
		return session.Close()
	}
	return nil
}

func (c *remoteMCPClient) ListTools(ctx context.Context, params *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error] {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return func(yield func(*mcp.Tool, error) bool) {
			yield(nil, fmt.Errorf("session not initialized"))
		}
	}

	return session.Tools(ctx, params)
}

func (c *remoteMCPClient) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	return session.CallTool(ctx, params)
}

// requestUserConsent requests user consent to start the OAuth flow via elicitation
func (c *remoteMCPClient) requestUserConsent(ctx context.Context) (bool, error) {
	result, err := c.requestElicitation(ctx, &mcp.ElicitParams{
		Message:         fmt.Sprintf("The MCP server at %s requires OAuth authorization. Do you want to proceed?", c.url),
		RequestedSchema: nil,
		Meta: map[string]any{
			"cagent/type":       "oauth_consent",
			"cagent/server_url": c.url,
		},
	})
	if err != nil {
		return false, err
	}

	slog.Debug("Elicitation response received", "result", result)

	return result.Action == "accept", nil
}
