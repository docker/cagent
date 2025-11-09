package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/filesystem"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/telemetry"
)

type buildFlags struct {
	opts oci.Options
}

func newBuildCmd() *cobra.Command {
	var flags buildFlags

	cmd := &cobra.Command{
		Use:   "build <agent-file> [docker-image-name]",
		Short: "Build a Docker image for the agent",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  flags.runBuildCommand,
	}

	cmd.PersistentFlags().BoolVar(&flags.opts.DryRun, "dry-run", false, "only print the generated Dockerfile")
	cmd.PersistentFlags().BoolVar(&flags.opts.Push, "push", false, "push the image")
	cmd.PersistentFlags().BoolVar(&flags.opts.NoCache, "no-cache", false, "Do not use cache when building the image")
	cmd.PersistentFlags().BoolVar(&flags.opts.Pull, "pull", false, "Always attempt to pull all referenced images")

	return cmd
}

func (f *buildFlags) runBuildCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("build", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())

	agentFilePath := args[0]
	dockerImageName := ""
	if len(args) > 1 {
		dockerImageName = args[1]
	}

	return oci.BuildDockerImage(ctx, out, agentFilePath, filesystem.AllowAll, dockerImageName, f.opts)
}
