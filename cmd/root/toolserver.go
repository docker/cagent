package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/toolserver"
)

type toolserverFlags struct {
	listenAddr string
	runConfig  config.RuntimeConfig
}

func newToolServerCmd() *cobra.Command {
	var flags toolserverFlags

	cmd := &cobra.Command{
		Use:   "tool-server <agent-file>|<registry-ref>",
		Short: "Start a lightweight server exposing agent tools for remote invocation",
		Long:  `Start a minimal HTTP server that exposes the tools defined in an agent configuration for remote invocation.`,
		Example: `  # Start tool server on default port
  cagent tool-server ./agent.yaml

  # Start on a specific port
  cagent tool-server ./agent.yaml --listen :9000

  # Listen on a Unix socket
  cagent tool-server ./agent.yaml --listen unix:///var/run/cagent.sock

  # Call a tool using curl
  curl -X POST http://localhost:8080/agents/root/tools/read_file \
    -H "Content-Type: application/json" \
    -d '{"arguments": "{\"path\": \"./README.md\"}"}'`,
		GroupID: "server",
		Args:    cobra.ExactArgs(1),
		RunE:    flags.runToolServerCommand,
		Hidden:  true,
	}

	cmd.PersistentFlags().StringVarP(&flags.listenAddr, "listen", "l", ":8080", "Address to listen on (host:port or unix:///path/to/socket)")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *toolserverFlags) runToolServerCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("tool-server", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())
	agentSource := args[0]

	source, err := config.Resolve(agentSource)
	if err != nil {
		return fmt.Errorf("resolving agent source: %w", err)
	}

	ln, err := server.Listen(ctx, f.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", f.listenAddr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	out.Println("Starting tool server...")

	s, err := toolserver.New(ctx, source, &f.runConfig)
	if err != nil {
		return fmt.Errorf("creating tool server: %w", err)
	}

	out.Println("Listening on " + ln.Addr().String())

	return s.Serve(ctx, ln)
}
