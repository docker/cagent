package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/tools"
)

type stdioMCPClient struct {
	command string
	args    []string
	env     []string
	session *mcp.ClientSession
}

func newStdioCmdClient(command string, args, env []string) *stdioMCPClient {
	return &stdioMCPClient{
		command: command,
		args:    args,
		env:     env,
	}
}

func (c *stdioMCPClient) Initialize(ctx context.Context, _ *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	// First, let's see if DD is running. This will help produce a better error message
	if c.command == "docker" && !desktop.IsDockerDesktopRunning(ctx) {
		return nil, errors.New("Docker Desktop is not running") //nolint:staticcheck // Don't lowercase Docker Desktop
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
	}, nil)

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	cmd.Env = c.env
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command: cmd,
	}, nil)
	if err != nil {
		return nil, err
	}

	c.session = session
	return c.session.InitializeResult(), nil
}

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

func (c *stdioMCPClient) SetElicitationHandler(tools.ElicitationHandler) {}

func (c *stdioMCPClient) SetOAuthSuccessHandler(func()) {}

func (c *stdioMCPClient) SetManagedOAuth(bool) {}
