package root

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

type debugFlags struct {
	runConfig config.RuntimeConfig
}

func newDebugCmd() *cobra.Command {
	var flags debugFlags

	cmd := &cobra.Command{
		Use: "debug",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "toolsets <agent-name>",
		Short: "Debug the toolsets of an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  flags.runDebugToolsetsCommand,
	})

	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *debugFlags) runDebugToolsetsCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"toolsets"}, args...))

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())
	agentFilename := args[0]

	team, err := teamloader.Load(ctx, agentFilename, f.runConfig)
	if err != nil {
		return err
	}

	for _, name := range team.AgentNames() {
		agent, err := team.Agent(name)
		if err != nil {
			slog.Error("Failed to get agent", "name", name, "error", err)
			continue
		}

		tools, err := agent.Tools(ctx)
		if err != nil {
			slog.Error("Failed to query tools", "name", agent.Name(), "error", err)
			continue
		}

		if len(tools) == 0 {
			out.Printf("No tools for %s\n", agent.Name())
			continue
		}

		out.Printf("%d tool(s) for %s:\n", len(tools), agent.Name())
		for _, tool := range tools {
			out.Println(" +", tool.Name)
		}
	}

	if err := team.StopToolSets(ctx); err != nil {
		slog.Error("Failed to stop tool sets", "error", err)
	}

	return err
}
