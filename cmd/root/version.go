package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

// NewVersionCmd creates a new version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  `Display the version, build time, and commit hash`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Track the version command
			telemetry.TrackCommand("version", args)

			fmt.Printf("cagent version %s\n", version.Version)
			fmt.Printf("Commit: %s\n", version.Commit)
		},
	}
}
