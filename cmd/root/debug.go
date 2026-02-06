package root

import (
	"log/slog"

	"github.com/goccy/go-yaml"
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
		Use:     "debug",
		Short:   "Debug tools",
		GroupID: "advanced",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "config <agent-file>|<registry-ref>",
		Short: "Print the canonical form of an agent's configuration file",
		Args:  cobra.ExactArgs(1),
		RunE:  flags.runDebugConfigCommand,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "toolsets <agent-file>|<registry-ref>",
		Short: "Debug the toolsets of an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  flags.runDebugToolsetsCommand,
	})

	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *debugFlags) runDebugConfigCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"config"}, args...))

	ctx := cmd.Context()
	agentFilename := args[0]

	agentSource, err := config.Resolve(agentFilename, f.runConfig.EnvProvider())
	if err != nil {
		return err
	}

	cfg, err := config.Load(ctx, agentSource)
	if err != nil {
		return err
	}

	return yaml.NewEncoder(cmd.OutOrStdout()).Encode(cfg)
}

func (f *debugFlags) runDebugToolsetsCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"toolsets"}, args...))

	ctx := cmd.Context()
	agentFilename := args[0]
	out := cli.NewPrinter(cmd.OutOrStdout())

	agentSource, err := config.Resolve(agentFilename, f.runConfig.EnvProvider())
	if err != nil {
		return err
	}

	team, err := teamloader.Load(ctx, agentSource, &f.runConfig)
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
			out.Println(" +", tool.Name, "-", tool.Description)
		}
	}

	if err := team.StopToolSets(ctx); err != nil {
		slog.Error("Failed to stop tool sets", "error", err)
	}

	return err
}
