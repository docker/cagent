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
	"github.com/docker/cagent/pkg/upstream"
)

type remoteMCPClient struct {
	session             *mcp.ClientSession
	url                 string
	transportType       string
	headers             map[string]string
	tokenStore          OAuthTokenStore
	elicitationHandler  tools.ElicitationHandler
	oauthSuccessHandler func()
	managed             bool
	mu                  sync.RWMutex
}

func newRemoteClient(url, transportType string, headers map[string]string, tokenStore OAuthTokenStore) *remoteMCPClient {
	slog.Debug("Creating remote MCP client", "url", url, "transport", transportType, "headers", headers)

	if tokenStore == nil {
		tokenStore = NewInMemoryTokenStore()
	}

	return &remoteMCPClient{
		url:           url,
		transportType: transportType,
		headers:       headers,
		tokenStore:    tokenStore,
		managed:       false,
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
		Action:  string(result.Action),
		Content: result.Content,
	}, nil
}

func (c *remoteMCPClient) Initialize(ctx context.Context, _ *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	// Create HTTP client with OAuth support
	httpClient := c.createHTTPClient()

	var transport mcp.Transport

	switch c.transportType {
	case "sse":
		transport = &mcp.SSEClientTransport{
			Endpoint:   c.url,
			HTTPClient: httpClient,
		}
	case "streamable", "streamable-http":
		transport = &mcp.StreamableClientTransport{
			Endpoint:             c.url,
			HTTPClient:           httpClient,
			DisableStandaloneSSE: true,
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

	// Connect to the MCP server
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

// createHTTPClient creates an HTTP client with custom headers and OAuth support.
// Header values may contain ${headers.NAME} placeholders that are resolved
// at request time from upstream headers stored in the request context.
func (c *remoteMCPClient) createHTTPClient() *http.Client {
	transport := c.headerTransport()

	// Then wrap with OAuth support
	transport = &oauthTransport{
		base:       transport,
		client:     c,
		tokenStore: c.tokenStore,
		baseURL:    c.url,
		managed:    c.managed,
	}

	return &http.Client{
		Transport: transport,
	}
}

func (c *remoteMCPClient) headerTransport() http.RoundTripper {
	if len(c.headers) > 0 {
		return upstream.NewHeaderTransport(http.DefaultTransport, c.headers)
	}
	return http.DefaultTransport
}

func (c *remoteMCPClient) Close(context.Context) error {
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

// ListPrompts retrieves available prompts from the remote MCP server
func (c *remoteMCPClient) ListPrompts(ctx context.Context, request *mcp.ListPromptsParams) iter.Seq2[*mcp.Prompt, error] {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return func(yield func(*mcp.Prompt, error) bool) {
			yield(nil, fmt.Errorf("session not initialized"))
		}
	}

	return session.Prompts(ctx, request)
}

// GetPrompt retrieves a specific prompt with arguments from the remote MCP server
func (c *remoteMCPClient) GetPrompt(ctx context.Context, request *mcp.GetPromptParams) (*mcp.GetPromptResult, error) {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	return session.GetPrompt(ctx, request)
}

// SetElicitationHandler sets the elicitation handler for remote MCP clients
// This allows the runtime to provide a handler that propagates elicitation requests
func (c *remoteMCPClient) SetElicitationHandler(handler tools.ElicitationHandler) {
	c.mu.Lock()
	c.elicitationHandler = handler
	c.mu.Unlock()
}

func (c *remoteMCPClient) SetOAuthSuccessHandler(handler func()) {
	c.mu.Lock()
	c.oauthSuccessHandler = handler
	c.mu.Unlock()
}

// SetManagedOAuth sets whether OAuth should be handled in managed mode
// In managed mode, the client handles the OAuth flow instead of the server
func (c *remoteMCPClient) SetManagedOAuth(managed bool) {
	c.mu.Lock()
	c.managed = managed
	c.mu.Unlock()
}
