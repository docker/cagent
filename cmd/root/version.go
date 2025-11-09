package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  `Display the version, build time, and commit hash`,
		Args:  cobra.NoArgs,
		Run:   runVersionCommand,
	}
}

func runVersionCommand(cmd *cobra.Command, args []string) {
	telemetry.TrackCommand("version", args)

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "cagent version %s\n", version.Version)
	fmt.Fprintf(out, "Commit: %s\n", version.Commit)
}
