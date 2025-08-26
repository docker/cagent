package root

import (
	"fmt"
	"log/slog"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/spf13/cobra"
)

func NewPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push <reference>",
		Short: "Push an artifact to an OCI registry",
		Long: `Push an artifact from the local content store to an OCI registry.

The local identifier can be either a reference (tag) or a digest that was returned
from the build command.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("push", args)
			return runPushCommand(args[0], args[1])
		},
	}

	return cmd
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
