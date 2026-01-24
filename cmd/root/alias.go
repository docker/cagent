package root

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/userconfig"
)

func newAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage aliases",
		Long:  "Create and manage aliases for agent configurations or catalog references.",
		Example: `  # Create an alias for a catalog agent
  cagent alias add code agentcatalog/notion-expert

  # Create an alias for a local agent file
  cagent alias add myagent ~/myagent.yaml

  # List all registered aliases
  cagent alias list

  # Remove an alias
  cagent alias remove code`,
		GroupID: "advanced",
	}

	cmd.AddCommand(newAliasAddCmd())
	cmd.AddCommand(newAliasListCmd())
	cmd.AddCommand(newAliasRemoveCmd())

	return cmd
}

type aliasAddFlags struct {
	yolo            bool
	model           string
	hideToolResults bool
}

func newAliasAddCmd() *cobra.Command {
	var flags aliasAddFlags

	cmd := &cobra.Command{
		Use:   "add <alias-name> <agent-path>",
		Short: "Add a new alias",
		Long: `Add a new alias for an agent configuration or catalog reference.

You can optionally specify runtime options that will be applied whenever
the alias is used:

  --yolo               Automatically approve all tool calls without prompting
  --model              Override the agent's model (format: [agent=]provider/model)
  --hide-tool-results  Hide tool call results in the TUI`,
		Example: `  # Create a simple alias
  cagent alias add code agentcatalog/notion-expert

  # Create an alias that always runs in yolo mode
  cagent alias add yolo-coder agentcatalog/coder --yolo

  # Create an alias with a specific model
  cagent alias add fast-coder agentcatalog/coder --model openai/gpt-4o-mini

  # Create an alias with hidden tool results
  cagent alias add quiet agentcatalog/coder --hide-tool-results

  # Create an alias with multiple options
  cagent alias add turbo agentcatalog/coder --yolo --model anthropic/claude-sonnet-4-0`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAliasAddCommand(cmd, args, &flags)
		},
	}

	cmd.Flags().BoolVar(&flags.yolo, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.Flags().StringVar(&flags.model, "model", "", "Override agent model (format: [agent=]provider/model)")
	cmd.Flags().BoolVar(&flags.hideToolResults, "hide-tool-results", false, "Hide tool call results in the TUI")

	return cmd
}

func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all registered aliases",
		Args:    cobra.NoArgs,
		RunE:    runAliasListCommand,
	}
}

func newAliasRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <alias-name>",
		Aliases: []string{"rm"},
		Short:   "Remove a registered alias",
		Args:    cobra.ExactArgs(1),
		RunE:    runAliasRemoveCommand,
	}
}

func runAliasAddCommand(cmd *cobra.Command, args []string, flags *aliasAddFlags) error {
	telemetry.TrackCommand("alias", append([]string{"add"}, args...))

	out := cli.NewPrinter(cmd.OutOrStdout())
	name := args[0]
	agentPath := args[1]

	// Load existing config
	cfg, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Expand tilde in path if it's a local file path
	absAgentPath, err := expandTilde(agentPath)
	if err != nil {
		return err
	}

	// Create alias with options
	alias := &userconfig.Alias{
		Path:            absAgentPath,
		Yolo:            flags.yolo,
		Model:           flags.model,
		HideToolResults: flags.hideToolResults,
	}

	// Store the alias
	if err := cfg.SetAlias(name, alias); err != nil {
		return err
	}

	// Save to file
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Printf("Alias '%s' created successfully\n", name)
	out.Printf("  Alias: %s\n", name)
	out.Printf("  Agent: %s\n", absAgentPath)
	if flags.yolo {
		out.Printf("  Yolo:  enabled\n")
	}
	if flags.model != "" {
		out.Printf("  Model: %s\n", flags.model)
	}
	if flags.hideToolResults {
		out.Printf("  Hide tool results: enabled\n")
	}

	if name == "default" {
		out.Printf("\nYou can now run: cagent run %s (or even cagent run)\n", name)
	} else {
		out.Printf("\nYou can now run: cagent run %s\n", name)
	}

	return nil
}

func runAliasListCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("alias", append([]string{"list"}, args...))

	out := cli.NewPrinter(cmd.OutOrStdout())

	cfg, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	allAliases := cfg.Aliases
	if len(allAliases) == 0 {
		out.Println("No aliases registered.")
		out.Println("\nCreate an alias with: cagent alias add <name> <agent-path>")
		return nil
	}

	out.Printf("Registered aliases (%d):\n\n", len(allAliases))

	// Sort aliases by name for consistent output
	names := make([]string, 0, len(allAliases))
	for name := range allAliases {
		names = append(names, name)
	}
	sort.Strings(names)

	// Find max name width for alignment (using display width for proper Unicode handling)
	maxLen := 0
	for _, name := range names {
		maxLen = max(maxLen, runewidth.StringWidth(name))
	}

	for _, name := range names {
		alias := allAliases[name]
		padding := strings.Repeat(" ", maxLen-runewidth.StringWidth(name))

		// Build options string
		var options []string
		if alias.Yolo {
			options = append(options, "yolo")
		}
		if alias.Model != "" {
			options = append(options, "model="+alias.Model)
		}
		if alias.HideToolResults {
			options = append(options, "hide-tool-results")
		}

		if len(options) > 0 {
			out.Printf("  %s%s → %s [%s]\n", name, padding, alias.Path, strings.Join(options, ", "))
		} else {
			out.Printf("  %s%s → %s\n", name, padding, alias.Path)
		}
	}

	out.Println("\nRun an alias with: cagent run <alias>")

	return nil
}

func runAliasRemoveCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("alias", append([]string{"remove"}, args...))

	out := cli.NewPrinter(cmd.OutOrStdout())
	name := args[0]

	cfg, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.DeleteAlias(name) {
		return fmt.Errorf("alias '%s' not found", name)
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Printf("Alias '%s' removed successfully\n", name)
	return nil
}

// expandTilde expands the tilde in a path to the user's home directory
func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	homeDir := paths.GetHomeDir()
	if homeDir == "" {
		return "", fmt.Errorf("failed to get user home directory")
	}

	return filepath.Join(homeDir, strings.TrimPrefix(path, "~/")), nil
}
