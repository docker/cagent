package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/version"
)

type ToolInput struct {
	Message string `json:"message" jsonschema:"the message to send to the agent"`
}

type ToolOutput struct {
	Response string `json:"response" jsonschema:"the response from the agent"`
}

func StartMCPServer(ctx context.Context, agentFilename, agentName string, runConfig *config.RuntimeConfig) error {
	slog.Debug("Starting MCP server", "agent", agentFilename)

	server, cleanup, err := createMCPServer(ctx, agentFilename, agentName, runConfig)
	if err != nil {
		return err
	}
	defer cleanup()

	slog.Debug("MCP server starting with stdio transport")

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server error: %w", err)
	}

	return nil
}

// StartHTTPServer starts a streaming HTTP MCP server on the given listener
func StartHTTPServer(ctx context.Context, agentFilename, agentName string, runConfig *config.RuntimeConfig, ln net.Listener) error {
	slog.Debug("Starting HTTP MCP server", "agent", agentFilename, "addr", ln.Addr())

	server, cleanup, err := createMCPServer(ctx, agentFilename, agentName, runConfig)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Printf("MCP HTTP server listening on http://%s\n", ln.Addr())

	httpServer := &http.Server{
		Handler: mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return server
		}, nil),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		return httpServer.Shutdown(context.Background())
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func createMCPServer(ctx context.Context, agentFilename, agentName string, runConfig *config.RuntimeConfig) (*mcp.Server, func(), error) {
	agentSource, err := config.Resolve(agentFilename, nil)
	if err != nil {
		return nil, nil, err
	}

	t, err := teamloader.Load(ctx, agentSource, runConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agents: %w", err)
	}

	cleanup := func() {
		if err := t.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "cagent",
		Version: version.Version,
	}, nil)

	agentNames := t.AgentNames()
	if agentName != "" {
		if !slices.Contains(agentNames, agentName) {
			cleanup()
			return nil, nil, fmt.Errorf("agent %s not found in %s", agentName, agentFilename)
		}
		agentNames = []string{agentName}
	}

	slog.Debug("Adding MCP tools for agents", "count", len(agentNames))

	for _, agentName := range agentNames {
		ag, err := t.Agent(agentName)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to get agent %s: %w", agentName, err)
		}

		description := cmp.Or(ag.Description(), fmt.Sprintf("Run the %s agent", agentName))

		slog.Debug("Adding MCP tool", "agent", agentName, "description", description)

		readOnly, err := isReadOnlyAgent(ctx, ag)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to determine if agent %s is read-only: %w", agentName, err)
		}

		toolDef := &mcp.Tool{
			Name:        agentName,
			Description: description,
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint: readOnly,
			},
			InputSchema:  tools.MustSchemaFor[ToolInput](),
			OutputSchema: tools.MustSchemaFor[ToolOutput](),
		}

		mcp.AddTool(server, toolDef, CreateToolHandler(t, agentName))
	}

	return server, cleanup, nil
}

func CreateToolHandler(t *team.Team, agentName string) func(context.Context, *mcp.CallToolRequest, ToolInput) (*mcp.CallToolResult, ToolOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, ToolOutput, error) {
		slog.Debug("MCP tool called", "agent", agentName, "message", input.Message)

		ag, err := t.Agent(agentName)
		if err != nil {
			return nil, ToolOutput{}, fmt.Errorf("failed to get agent: %w", err)
		}

		sess := session.New(
			session.WithTitle("MCP tool call"),
			session.WithMaxIterations(ag.MaxIterations()),
			session.WithUserMessage(input.Message),
			session.WithToolsApproved(true),
		)

		rt, err := runtime.New(t,
			runtime.WithCurrentAgent(agentName),
		)
		if err != nil {
			return nil, ToolOutput{}, fmt.Errorf("failed to create runtime: %w", err)
		}

		_, err = rt.Run(ctx, sess)
		if err != nil {
			slog.Error("Agent execution failed", "agent", agentName, "error", err)
			return nil, ToolOutput{}, fmt.Errorf("agent execution failed: %w", err)
		}

		result := cmp.Or(sess.GetLastAssistantMessageContent(), "No response from agent")

		slog.Debug("Agent execution completed", "agent", agentName, "response_length", len(result))

		return nil, ToolOutput{Response: result}, nil
	}
}

func isReadOnlyAgent(ctx context.Context, ag *agent.Agent) (bool, error) {
	allTools, err := ag.Tools(ctx)
	if err != nil {
		return false, err
	}

	for _, tool := range allTools {
		if !tool.Annotations.ReadOnlyHint {
			return false, nil
		}
	}

	return true, nil
}
