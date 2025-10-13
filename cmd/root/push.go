package root

import (
	"fmt"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/telemetry"
)

func NewPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <agent file> <reference>",
		Short: "Push an agent to an OCI registry",
		Long: `Push an agent to an OCI registry.

The local identifier can be either a reference (tag) or a digest that was returned
from the build command.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("push", args)
			return runPushCommand(args[0], args[1])
		},
	}
}

func runPushCommand(filePath, tag string) error {
	store, err := content.NewStore()
	if err != nil {
		return err
	}

	_, err = oci.PackageFileAsOCIToStore(filePath, tag, store)
	if err != nil {
		return fmt.Errorf("failed to build artifact: %w", err)
	}

	slog.Debug("Starting push", "registry_ref", tag)

	fmt.Printf("Pushing agent %s to %s\n", filePath, tag)

	var opts []crane.Option

	err = remote.Push(tag, opts...)
	if err != nil {
		return fmt.Errorf("failed to push artifact: %w", err)
	}

	fmt.Printf("Successfully pushed artifact to %s\n", tag)
	return nil
}
