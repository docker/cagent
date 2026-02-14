package root

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/sessiontitle"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

type debugFlags struct {
	modelOverrides []string
	runConfig      config.RuntimeConfig
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
	titleCmd := &cobra.Command{
		Use:   "title <agent-file>|<registry-ref> <question>",
		Short: "Generate a session title from a question",
		Args:  cobra.ExactArgs(2),
		RunE:  flags.runDebugTitleCommand,
	}
	titleCmd.Flags().StringArrayVar(&flags.modelOverrides, "model", nil, "Override agent model: [agent=]provider/model (repeatable)")
	cmd.AddCommand(titleCmd)

	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

// resolveSource resolves an agent file reference to a config source.
func (f *debugFlags) resolveSource(agentFilename string) (config.Source, error) {
	return config.Resolve(agentFilename, f.runConfig.EnvProvider())
}

// loadTeam loads an agent team from the given agent file and returns
// a cleanup function that must be deferred by the caller.
func (f *debugFlags) loadTeam(ctx context.Context, agentFilename string, opts ...teamloader.Opt) (*team.Team, func(), error) {
	agentSource, err := f.resolveSource(agentFilename)
	if err != nil {
		return nil, nil, err
	}

	t, err := teamloader.Load(ctx, agentSource, &f.runConfig, opts...)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if err := t.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}

	return t, cleanup, nil
}

func (f *debugFlags) runDebugConfigCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"config"}, args...))

	agentSource, err := f.resolveSource(args[0])
	if err != nil {
		return err
	}

	cfg, err := config.Load(cmd.Context(), agentSource)
	if err != nil {
		return err
	}

	return yaml.NewEncoder(cmd.OutOrStdout()).Encode(cfg)
}

func (f *debugFlags) runDebugToolsetsCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"toolsets"}, args...))

	ctx := cmd.Context()

	t, cleanup, err := f.loadTeam(ctx, args[0])
	if err != nil {
		return err
	}
	defer cleanup()

	out := cli.NewPrinter(cmd.OutOrStdout())

	for _, name := range t.AgentNames() {
		agent, err := t.Agent(name)
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

	return nil
}

func (f *debugFlags) runDebugTitleCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("debug", append([]string{"title"}, args...))

	ctx := cmd.Context()

	t, cleanup, err := f.loadTeam(ctx, args[0], teamloader.WithModelOverrides(f.modelOverrides))
	if err != nil {
		return err
	}
	defer cleanup()

	agent, err := t.DefaultAgent()
	if err != nil {
		return err
	}

	model := agent.Model()
	if model == nil {
		return fmt.Errorf("agent %q has no model configured", agent.Name())
	}

	// Use the same title generation code path as the TUI (see runTUI in new.go)
	gen := sessiontitle.New(model, agent.FallbackModels()...)

	title, err := gen.Generate(ctx, "debug", []string{args[1]})
	if err != nil {
		return fmt.Errorf("generating title: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), title)

	return nil
}
