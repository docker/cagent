package mcp

import (
	"context"
	"fmt"
	"iter"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func (c *stdioMCPClient) Start(context.Context) error {
	return nil
}

func (c *stdioMCPClient) Initialize(ctx context.Context, _ *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
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
