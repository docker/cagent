package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/pkg/oci"
)

var push bool

func NewBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "build <agent-file> [docker-image-name]",
		Short:  "Build a Docker image for the agent",
		Args:   cobra.MinimumNArgs(1),
		RunE:   runBuildCommand,
		Hidden: true,
	}

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

	return oci.BuildDockerImage(cmd.Context(), agentFilePath, dockerImageName, push)
}
