package root

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/aliases"
	"github.com/docker/cagent/pkg/telemetry"
)

// NewAliasCmd creates a new alias command for managing aliases
func NewAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage aliases for agents",
		Long:  `Create and manage aliases for agent configurations or catalog references.`,
		Example: `  # Create an alias for a catalog agent
  cagent alias add code agentcatalog/notion-expert

  # Create an alias for a local agent file
  cagent alias add myagent ~/myagent.yaml

  # List all registered aliases
  cagent alias list

  # Remove an alias
  cagent alias remove code`,
	}

	cmd.AddCommand(newAliasAddCmd())
	cmd.AddCommand(newAliasListCmd())
	cmd.AddCommand(newAliasRemoveCmd())

	return cmd
}

// newAliasAddCmd creates the add subcommand
func newAliasAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <alias-name> <agent-path>",
		Short: "Add a new alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			telemetry.TrackCommand("alias", append([]string{"add"}, args...))
			return createAlias(args[0], args[1])
		},
	}
}

// newAliasListCmd creates the list subcommand
func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all registered aliases",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			telemetry.TrackCommand("alias", []string{"list"})
			return listAliases()
		},
	}
}

// newAliasRemoveCmd creates the remove subcommand
func newAliasRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <alias-name>",
		Aliases: []string{"rm"},
		Short:   "Remove a registered alias",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			telemetry.TrackCommand("alias", append([]string{"remove"}, args...))
			return removeAlias(args[0])
		},
	}
}

// createAlias creates a new alias
func createAlias(name, agentPath string) error {
	// Load existing aliases
	s, err := aliases.Load()
	if err != nil {
		return fmt.Errorf("failed to load aliases: %w", err)
	}

	// Expand tilde in path if it's a local file path
	absAgentPath, err := expandTilde(agentPath)
	if err != nil {
		return err
	}

	// Store the alias
	s.Set(name, absAgentPath)

	// Save to file
	if err := s.Save(); err != nil {
		return fmt.Errorf("failed to save aliases: %w", err)
	}

	fmt.Printf("Alias '%s' created successfully\n", name)
	fmt.Printf("  Alias: %s\n", name)
	fmt.Printf("  Agent: %s\n", absAgentPath)
	fmt.Printf("\nYou can now run: cagent run %s\n", name)

	return nil
}

// removeAlias removes an alias
func removeAlias(name string) error {
	s, err := aliases.Load()
	if err != nil {
		return fmt.Errorf("failed to load aliases: %w", err)
	}

	if !s.Delete(name) {
		return fmt.Errorf("alias '%s' not found", name)
	}

	if err := s.Save(); err != nil {
		return fmt.Errorf("failed to save aliases: %w", err)
	}

	fmt.Printf("Alias '%s' removed successfully\n", name)
	return nil
}

func listAliases() error {
	s, err := aliases.Load()
	if err != nil {
		return fmt.Errorf("failed to load aliases: %w", err)
	}

	allAliases := s.List()
	if len(allAliases) == 0 {
		fmt.Println("No aliases registered.")
		fmt.Println("\nCreate an alias with: cagent alias add <name> <agent-path>")
		return nil
	}

	fmt.Printf("Registered aliases (%d):\n\n", len(allAliases))

	// Sort aliases by name for consistent output
	names := make([]string, 0, len(allAliases))
	for name := range allAliases {
		names = append(names, name)
	}
	sort.Strings(names)

	// Find max name length for alignment
	maxLen := 0
	for _, name := range names {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range names {
		path := allAliases[name]
		padding := strings.Repeat(" ", maxLen-len(name))
		fmt.Printf("  %s%s → %s\n", name, padding, path)
	}

	fmt.Printf("\nRun an alias with: cagent run <alias>\n")

	return nil
}

// expandTilde expands the tilde in a path to the user's home directory
func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, strings.TrimPrefix(path, "~/")), nil
}
