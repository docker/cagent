package root

import (
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  "Display the version and commit hash",
		Args:  cobra.NoArgs,
		Run:   runVersionCommand,
	}
}

func runVersionCommand(cmd *cobra.Command, args []string) {
	telemetry.TrackCommand("version", args)

	out := cli.NewPrinter(cmd.OutOrStdout())

	commandName := "docker-agent"
	if cmd.Parent() != nil {
		commandName = cmd.Parent().Name()
	}
	if !plugin.RunningStandalone() {
		commandName = "docker " + commandName
	}
	out.Printf("%s version %s\n", commandName, version.Version)
	out.Printf("Commit: %s\n", version.Commit)
}
