package root

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/telemetry"
)

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <registry-ref>",
		Short: "Pull an artifact from Docker Hub",
		Long:  `Pull an artifact from Docker Hub`,
		Args:  cobra.ExactArgs(1),
		RunE:  runPullCommand,
	}
}

func runPullCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("pull", args)

	out := cli.NewPrinter(cmd.OutOrStdout())
	registryRef := args[0]
	slog.Debug("Starting pull", "registry_ref", registryRef)

	out.Println("Pulling agent", registryRef)

	var opts []crane.Option
	_, err := remote.Pull(registryRef, opts...)
	if err != nil {
		return fmt.Errorf("failed to pull artifact: %w", err)
	}

	yamlFile, err := agentfile.FromStore(registryRef)
	if err != nil {
		return fmt.Errorf("failed to get agent yaml: %w", err)
	}

	agentName := strings.ReplaceAll(registryRef, "/", "_")
	fileName := agentName + ".yaml"

	if err := os.WriteFile(fileName, []byte(yamlFile), 0o644); err != nil {
		return err
	}

	out.Printf("Agent saved to %s\n", fileName)

	return nil
}
