package root

import (
	"fmt"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/internal/version"
	"github.com/spf13/cobra"
)

// NewVersionCmd creates a new version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  `Display the version, build time, and commit hash`,
		Run: func(cmd *cobra.Command, args []string) {
			// Track the version command
			telemetry.TrackCommand("version", args)

			fmt.Printf("cagent version %s\n", version.Version)
			fmt.Printf("Build time: %s\n", version.BuildTime)
			fmt.Printf("Commit: %s\n", version.Commit)
		},
	}
}
