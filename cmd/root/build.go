package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/telemetry"
)

var (
	push   bool
	dryRun bool
)

func NewBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "build <agent-file> [docker-image-name]",
		Short:  "Build a Docker image for the agent",
		Args:   cobra.MinimumNArgs(1),
		RunE:   runBuildCommand,
		Hidden: true,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "only print the generated Dockerfile")
	cmd.PersistentFlags().BoolVar(&push, "push", false, "push the image")

	return cmd
}

func runBuildCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("build", args)

	agentFilePath := args[0]
	dockerImageName := ""
	if len(args) > 1 {
		dockerImageName = args[1]
	}

	return oci.BuildDockerImage(cmd.Context(), agentFilePath, dockerImageName, dryRun, push)
}
