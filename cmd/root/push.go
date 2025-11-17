package root

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

func newPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "push <agent-file> <registry-ref>",
		Short:   "Push an agent to an OCI registry",
		Long:    "Push an agent configuration file to an OCI registry",
		GroupID: "core",
		Args:    cobra.ExactArgs(2),
		RunE:    runPushCommand,
	}

	cmd.Flags().String("version", "", "Version to inject into the agent metadata (auto-incremented if not provided)")

	return cmd
}

func runPushCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("push", args)

	filePath := args[0]
	tag := args[1]
	out := cli.NewPrinter(cmd.OutOrStdout())

	versionFlag, _ := cmd.Flags().GetString("version")

	agentName := getAgentNameFromPath(filePath)

	versionInfo, err := version.Detect(version.DetectOptions{
		ExplicitVersion: versionFlag,
		AgentName:       agentName,
		WorkingDir:      "",
	})
	if err != nil {
		return fmt.Errorf("failed to detect version: %w", err)
	}

	out.Printf("Using version %s\n", versionInfo.FormatForDisplay())

	store, err := content.NewStore()
	if err != nil {
		return err
	}

	versionedTag := createVersionedTag(tag, versionInfo.Version)
	latestTag := createLatestTag(tag)

	_, err = oci.PackageFileAsOCIToStoreWithVersion(filePath, versionedTag, store, versionInfo)
	if err != nil {
		return fmt.Errorf("failed to build artifact: %w", err)
	}

	_, err = oci.PackageFileAsOCIToStoreWithVersion(filePath, latestTag, store, versionInfo)
	if err != nil {
		return fmt.Errorf("failed to build artifact with latest tag: %w", err)
	}

	if versionInfo.Source == version.SourceCounter {
		if err := version.UpdateVersion(agentName, versionInfo.Version, ""); err != nil {
			slog.Warn("Failed to update version state", "error", err)
		}
	}

	out.Printf("Pushing agent %s to %s\n", filePath, versionedTag)

	slog.Debug("Starting push", "registry_ref", versionedTag)
	err = remote.Push(versionedTag)
	if err != nil {
		return fmt.Errorf("failed to push versioned artifact: %w", err)
	}

	out.Printf("Pushing agent %s to %s\n", filePath, latestTag)

	slog.Debug("Starting push", "registry_ref", latestTag)
	err = remote.Push(latestTag)
	if err != nil {
		return fmt.Errorf("failed to push latest artifact: %w", err)
	}

	out.Printf("Successfully pushed artifact to:\n")
	out.Printf("  %s\n", versionedTag)
	out.Printf("  %s\n", latestTag)
	return nil
}

// getAgentNameFromPath extracts agent name from file path for version tracking
func getAgentNameFromPath(filePath string) string {
	base := filepath.Base(filePath)
	name := base[:len(base)-len(filepath.Ext(base))]
	return name
}

// createVersionedTag creates a versioned tag from the base tag and version
func createVersionedTag(tag, ver string) string {
	if strings.Contains(tag, ":") {
		parts := strings.Split(tag, ":")
		return parts[0] + ":" + ver
	}
	return tag + ":" + ver
}

// createLatestTag creates a latest tag from the base tag
func createLatestTag(tag string) string {
	if strings.Contains(tag, ":") {
		parts := strings.Split(tag, ":")
		return parts[0] + ":latest"
	}
	return tag + ":latest"
}
