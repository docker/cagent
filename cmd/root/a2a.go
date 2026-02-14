package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/a2a"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/telemetry"
)

type a2aFlags struct {
	agentName  string
	listenAddr string
	runConfig  config.RuntimeConfig
}

func newA2ACmd() *cobra.Command {
	var flags a2aFlags

	cmd := &cobra.Command{
		Use:   "a2a <agent-file>|<registry-ref>",
		Short: "Start an agent as an A2A (Agent-to-Agent) server",
		Long:  "Start an A2A server that exposes the agent via the Agent-to-Agent protocol",
		Example: `  cagent a2a ./agent.yaml
  cagent a2a agentcatalog/pirate --listen 127.0.0.1:9090`,
		Args:    cobra.ExactArgs(1),
		GroupID: "server",
		RunE:    flags.runA2ACommand,
	}

	cmd.PersistentFlags().StringVarP(&flags.agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().StringVarP(&flags.listenAddr, "listen", "l", "127.0.0.1:8082", "Address to listen on")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *a2aFlags) runA2ACommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("a2a", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())
	agentFilename := args[0]

	// Listen as early as possible
	ln, err := server.Listen(ctx, f.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", f.listenAddr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	out.Println("Listening on", ln.Addr().String())
	return a2a.Run(ctx, agentFilename, f.agentName, &f.runConfig, ln)
}
