package root

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/mcp"
	"github.com/docker/cagent/pkg/telemetry"
)

type mcpFlags struct {
	workingDir string
	runConfig  config.RuntimeConfig
}

func newMCPCmd() *cobra.Command {
	var flags mcpFlags

	cmd := &cobra.Command{
		Use:   "mcp <agent-file>",
		Short: "Start an MCP (Model Context Protocol) server",
		Long:  `Start an MCP server that exposes agents as MCP tools via stdio`,
		Example: `  cagent mcp ./agent.yaml
  cagent mcp ./team.yaml
  cagent mcp agentcatalog/pirate`,
		Args: cobra.ExactArgs(1),
		RunE: flags.runMCPCommand,
	}

	cmd.PersistentFlags().StringVar(&flags.workingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *mcpFlags) runMCPCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("mcp", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(io.Discard)

	if err := setupWorkingDirectory(f.workingDir); err != nil {
		return err
	}

	return mcp.StartMCPServer(ctx, out, args[0], f.runConfig)
}
