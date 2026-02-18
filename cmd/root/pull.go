package root

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/telemetry"
)

type pullFlags struct {
	force bool
}

func newPullCmd() *cobra.Command {
	var flags pullFlags

	cmd := &cobra.Command{
		Use:   "pull <registry-ref>",
		Short: "Pull an agent from an OCI registry",
		Long:  "Pull an agent configuration file from an OCI registry",
		Args:  cobra.ExactArgs(1),
		RunE:  flags.runPullCommand,
	}

	cmd.PersistentFlags().BoolVar(&flags.force, "force", false, "Force pull even if the configuration already exists locally")

	return cmd
}

func (f *pullFlags) runPullCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("share", append([]string{"pull"}, args...))

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())
	registryRef := args[0]
	slog.Debug("Starting pull", "registry_ref", registryRef)

	out.Println("Pulling agent", registryRef)

	_, err := remote.Pull(ctx, registryRef, f.force)
	if err != nil {
		return fmt.Errorf("failed to pull artifact: %w", err)
	}

	store, err := content.NewStore()
	if err != nil {
		return fmt.Errorf("failed to open content store: %w", err)
	}
	yamlFile, err := store.GetArtifact(registryRef)
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
