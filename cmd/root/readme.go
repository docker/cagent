package root

import (
	"fmt"
	"os"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/pkg/config"
	"github.com/spf13/cobra"
)

// NewReadmeCmd creates a command that prints the README of an agent.
func NewReadmeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "readme <agent-name>",
		Short: "Print the README of an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  readmeAgentCommand,
	}

	return cmd
}

func readmeAgentCommand(_ *cobra.Command, args []string) error {
	telemetry.TrackCommand("readme", args)

	agentFilename := args[0]

	// Get current working directory as the base directory for security validation
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Use the secure config loader to prevent directory traversal attacks
	cfg, err := config.LoadConfigSecure(agentFilename, cwd)
	if err != nil {
		return err
	}

	_, err = fmt.Print(cfg.Metadata.Readme)
	return err
}
