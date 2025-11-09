package root

import (
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/filesystem"
	"github.com/docker/cagent/pkg/telemetry"
)

func newPrintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "print <agent-file>",
		Short: "Print the canonical form of an agent file",
		Args:  cobra.ExactArgs(1),
		RunE:  runPrintCommand,
	}
}

func runPrintCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("print", args)

	agentFilename := args[0]

	cfg, err := config.LoadConfig(agentFilename, filesystem.AllowAll)
	if err != nil {
		return err
	}

	return yaml.NewEncoder(cmd.OutOrStdout()).Encode(cfg)
}
