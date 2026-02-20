package root

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/telemetry"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <agent-file> <registry-ref>",
		Short: "Push an agent to an OCI registry",
		Long:  "Push an agent configuration file to an OCI registry",
		Args:  cobra.ExactArgs(2),
		RunE:  runPushCommand,
	}
}

func runPushCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("share", append([]string{"push"}, args...))

	ctx := cmd.Context()
	agentFilename := args[0]
	tag := args[1]
	out := cli.NewPrinter(cmd.OutOrStdout())

	store, err := content.NewStore()
	if err != nil {
		return err
	}

	agentSource, err := config.Resolve(agentFilename, nil)
	if err != nil {
		return fmt.Errorf("resolving agent file: %w", err)
	}

	_, err = oci.PackageFileAsOCIToStore(ctx, agentSource, tag, store)
	if err != nil {
		return fmt.Errorf("failed to build artifact: %w", err)
	}

	slog.Debug("Starting push", "registry_ref", tag)

	out.Printf("Pushing agent %s to %s\n", agentFilename, tag)

	err = remote.Push(tag)
	if err != nil {
		return fmt.Errorf("failed to push artifact: %w", err)
	}

	out.Printf("Successfully pushed artifact to %s\n", tag)
	return nil
}
