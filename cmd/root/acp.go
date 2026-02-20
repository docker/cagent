package root

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/acp"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/telemetry"
)

type acpFlags struct {
	runConfig config.RuntimeConfig
	sessionDB string
}

func newACPCmd() *cobra.Command {
	var flags acpFlags

	cmd := &cobra.Command{
		Use:   "acp <agent-file>|<registry-ref>",
		Short: "Start an agent as an ACP (Agent Client Protocol) server",
		Long:  "Start an ACP server that exposes the agent via the Agent Client Protocol",
		Example: `  cagent serve acp ./agent.yaml
  cagent serve acp ./team.yaml
  cagent serve acp agentcatalog/pirate`,
		Args: cobra.ExactArgs(1),
		RunE: flags.runACPCommand,
	}

	cmd.Flags().StringVarP(&flags.sessionDB, "session-db", "s", filepath.Join(paths.GetHomeDir(), ".cagent", "session.db"), "Path to the session database")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *acpFlags) runACPCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("serve", append([]string{"acp"}, args...))

	ctx := cmd.Context()
	agentFilename := args[0]

	// Expand tilde in session database path
	sessionDB, err := expandTilde(f.sessionDB)
	if err != nil {
		return err
	}

	return acp.Run(ctx, agentFilename, cmd.InOrStdin(), cmd.OutOrStdout(), &f.runConfig, sessionDB)
}
