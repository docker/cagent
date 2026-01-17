package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/tools"
)

type stdioMCPClient struct {
	command            string
	args               []string
	env                []string
	session            *mcp.ClientSession
	cwd                string
	elicitationHandler tools.ElicitationHandler
	mu                 sync.RWMutex
}

func newStdioCmdClient(command string, args, env []string, cwd string) *stdioMCPClient {
	return &stdioMCPClient{
		command: command,
		args:    args,
		env:     env,
		cwd:     cwd,
	}
}

func (c *stdioMCPClient) Initialize(ctx context.Context, _ *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	// First, let's see if DD is running. This will help produce a better error message
	// Skip this check on Linux where Docker runs natively without Docker Desktop
	if c.command == "docker" && runtime.GOOS != "linux" && !desktop.IsDockerDesktopRunning(ctx) {
		return nil, errors.New("Docker Desktop is not running") //nolint:staticcheck // Don't lowercase Docker Desktop
	}

	// Create client options with elicitation support
	opts := &mcp.ClientOptions{
		ElicitationHandler: c.handleElicitationRequest,
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
	}, opts)

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	cmd.Env = c.env
	cmd.Dir = c.cwd
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command: cmd,
	}, nil)
	if err != nil {
		return nil, err
	}

	c.session = session
	return c.session.InitializeResult(), nil
}

// handleElicitationRequest forwards incoming elicitation requests from the MCP server
func (c *stdioMCPClient) handleElicitationRequest(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	slog.Debug("Received elicitation request from stdio MCP server", "message", req.Params.Message)

	c.mu.RLock()
	handler := c.elicitationHandler
	c.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("no elicitation handler configured")
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		return nil, fmt.Errorf("elicitation failed: %w", err)
	}

	return &mcp.ElicitResult{
		Action:  string(result.Action),
		Content: result.Content,
	}, nil
}

// SetElicitationHandler sets the elicitation handler for stdio MCP clients
func (c *stdioMCPClient) SetElicitationHandler(handler tools.ElicitationHandler) {
	c.mu.Lock()
	c.elicitationHandler = handler
	c.mu.Unlock()
}

// SetOAuthSuccessHandler is a no-op for stdio clients (OAuth not supported)
func (c *stdioMCPClient) SetOAuthSuccessHandler(func()) {}

// SetManagedOAuth is a no-op for stdio clients (OAuth not supported)
func (c *stdioMCPClient) SetManagedOAuth(bool) {}

func (c *stdioMCPClient) Close(context.Context) error {
	if c.session == nil {
		return nil
	}

	return c.session.Close()
}

func (c *stdioMCPClient) ListTools(ctx context.Context, request *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error] {
	if c.session == nil {
		return func(yield func(*mcp.Tool, error) bool) {
			yield(nil, fmt.Errorf("session not initialized"))
		}
	}

	return c.session.Tools(ctx, request)
}

func (c *stdioMCPClient) CallTool(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	return c.session.CallTool(ctx, request)
}

// ListPrompts retrieves available prompts from the MCP server via stdio transport
func (c *stdioMCPClient) ListPrompts(ctx context.Context, request *mcp.ListPromptsParams) iter.Seq2[*mcp.Prompt, error] {
	if c.session == nil {
		return func(yield func(*mcp.Prompt, error) bool) {
			yield(nil, fmt.Errorf("session not initialized"))
		}
	}

	return c.session.Prompts(ctx, request)
}

// GetPrompt retrieves a specific prompt with arguments from the MCP server via stdio transport
func (c *stdioMCPClient) GetPrompt(ctx context.Context, request *mcp.GetPromptParams) (*mcp.GetPromptResult, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	return c.session.GetPrompt(ctx, request)
}
