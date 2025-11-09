package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/cli"
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

func StartMCPServer(ctx context.Context, out *cli.Printer, agentFilename string, runConfig config.RuntimeConfig) error {
	slog.Debug("Starting MCP server", "agent", agentFilename)

	agentFilename, err := agentfile.Resolve(ctx, out, agentFilename)
	if err != nil {
		return err
	}

	t, err := teamloader.Load(ctx, agentFilename, runConfig)
	if err != nil {
		return fmt.Errorf("failed to load agents: %w", err)
	}

	defer func() {
		if err := t.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "cagent",
		Version: version.Version,
	}, nil)

	agentNames := t.AgentNames()
	slog.Debug("Adding MCP tools for agents", "count", len(agentNames))

	for _, agentName := range agentNames {
		ag, err := t.Agent(agentName)
		if err != nil {
			return fmt.Errorf("failed to get agent %s: %w", agentName, err)
		}

		description := ag.Description()
		if description == "" {
			description = fmt.Sprintf("Run the %s agent", agentName)
		}

		slog.Debug("Adding MCP tool", "agent", agentName, "description", description)

		readOnly, err := isReadOnlyAgent(ctx, ag)
		if err != nil {
			return fmt.Errorf("failed to determine if agent %s is read-only: %w", agentName, err)
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

		mcp.AddTool(server, toolDef, CreateToolHandler(t, agentName, agentFilename))
	}

	slog.Debug("MCP server starting with stdio transport")

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server error: %w", err)
	}

	return nil
}

func CreateToolHandler(t *team.Team, agentName, agentFilename string) func(context.Context, *mcp.CallToolRequest, ToolInput) (*mcp.CallToolResult, ToolOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, ToolOutput, error) {
		slog.Debug("MCP tool called", "agent", agentName, "message", input.Message)

		ag, err := t.Agent(agentName)
		if err != nil {
			return nil, ToolOutput{}, fmt.Errorf("failed to get agent: %w", err)
		}

		sess := session.New(
			session.WithTitle("MCP tool call"),
			session.WithMaxIterations(ag.MaxIterations()),
			session.WithUserMessage(agentFilename, input.Message),
			session.WithToolsApproved(true),
		)

		rt, err := runtime.New(t,
			runtime.WithCurrentAgent(agentName),
			runtime.WithRootSessionID(sess.ID),
		)
		if err != nil {
			return nil, ToolOutput{}, fmt.Errorf("failed to create runtime: %w", err)
		}

		_, err = rt.Run(ctx, sess)
		if err != nil {
			slog.Error("Agent execution failed", "agent", agentName, "error", err)
			return nil, ToolOutput{}, fmt.Errorf("agent execution failed: %w", err)
		}

		result := sess.GetLastAssistantMessageContent()
		if result == "" {
			result = "No response from agent"
		}

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
