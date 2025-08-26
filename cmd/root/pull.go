package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/pkg/remote"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/spf13/cobra"
)

func NewPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull <registry-ref>",
		Short: "Pull an artifact from Docker Hub",
		Long:  `Pull an artifact from Docker Hub`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("pull", args)
			return runPullCommand(args[0])
		},
	}

	return cmd
}

func runPullCommand(registryRef string) error {
	slog.Debug("Starting pull", "registry_ref", registryRef)

	fmt.Println("Pulling agent ", registryRef)

	var opts []crane.Option
	_, err := remote.Pull(registryRef, opts...)
	if err != nil {
		return fmt.Errorf("failed to pull artifact: %w", err)
	}

	yamlFile, err := fromStore(registryRef)
	if err != nil {
		return fmt.Errorf("failed to get agent yaml: %w", err)
	}

	agentName := strings.ReplaceAll(registryRef, "/", "_")
	fileName := filepath.Join(agentsDir, agentName+".yaml")

	if err := os.WriteFile(fileName, []byte(yamlFile), 0o644); err != nil {
		return err
	}

	fmt.Printf("Agent saved to %s\n", fileName)

	return nil
}
