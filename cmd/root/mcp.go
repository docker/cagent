package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/mcp"
	"github.com/docker/cagent/pkg/telemetry"
)

type mcpFlags struct {
	agentName  string
	http       bool
	listenAddr string
	runConfig  config.RuntimeConfig
}

func newMCPCmd() *cobra.Command {
	var flags mcpFlags

	cmd := &cobra.Command{
		Use:   "mcp <agent-file>|<registry-ref>",
		Short: "Start an agent as an MCP (Model Context Protocol) server",
		Long:  "Start an MCP server that exposes the agent via the Model Context Protocol. By default, uses stdio transport. Use --http to start a streaming HTTP server instead.",
		Example: `  cagent mcp ./agent.yaml
  cagent mcp ./team.yaml
  cagent mcp agentcatalog/pirate
  cagent mcp ./agent.yaml --http --listen 127.0.0.1:9090`,
		Args:    cobra.ExactArgs(1),
		GroupID: "server",
		RunE:    flags.runMCPCommand,
	}

	cmd.PersistentFlags().StringVarP(&flags.agentName, "agent", "a", "", "Name of the agent to run (all agents if not specified)")
	cmd.PersistentFlags().BoolVar(&flags.http, "http", false, "Use streaming HTTP transport instead of stdio")
	cmd.PersistentFlags().StringVarP(&flags.listenAddr, "listen", "l", "127.0.0.1:8081", "Address to listen on")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *mcpFlags) runMCPCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("mcp", args)

	ctx := cmd.Context()
	agentFilename := args[0]

	if !f.http {
		return mcp.StartMCPServer(ctx, agentFilename, f.agentName, &f.runConfig)
	}

	ln, err := listenAndCloseOnCancel(ctx, f.listenAddr)
	if err != nil {
		return err
	}

	return mcp.StartHTTPServer(ctx, agentFilename, f.agentName, &f.runConfig, ln)
}
