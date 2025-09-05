package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/docker/cagent/internal/config"
	"github.com/docker/cagent/internal/telemetry"
	"github.com/spf13/cobra"
)

var (
	agentName  string
	debugMode  bool
	enableOtel bool
)

// isFirstRun checks if this is the first time cagent is being run
// It creates a marker file in the user's config directory
func isFirstRun() bool {
	configDir := config.GetConfigDir()
	markerFile := filepath.Join(configDir, ".cagent_first_run")

	// Check if marker file exists
	if _, err := os.Stat(markerFile); err == nil {
		return false // File exists, not first run
	}

	// Create marker file to indicate this run has happened
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false // Can't create config dir, assume not first run
	}

	if err := os.WriteFile(markerFile, []byte(""), 0o644); err != nil {
		return false // Can't create marker file, assume not first run
	}

	return true // Successfully created marker, this is first run
}

// NewRootCmd creates the root command for cagent
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cagent",
		Short: "cagent - AI agent runner",
		Long:  `cagent is a command-line tool for running AI agents`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.DisplayName() != "exec" && os.Getenv("CAGENT_HIDE_FEEDBACK_LINK") != "1" {
				_, _ = cmd.OutOrStdout().Write([]byte("\nFor any feedback, please visit: " + FeedbackLink + "\n\n"))
			}

			telemetry.SetGlobalTelemetryDebugMode(debugMode)
			if debugMode {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}
			return nil
		},
		// If no subcommand is specified, show help
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add persistent debug flag available to all commands
	cmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVarP(&enableOtel, "otel", "o", false, "Enable OpenTelemetry tracing")

	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(NewRunCmd())
	cmd.AddCommand(NewExecCmd())
	cmd.AddCommand(NewTuiCmd())
	cmd.AddCommand(NewNewCmd())
	cmd.AddCommand(NewApiCmd())
	cmd.AddCommand(NewEvalCmd())
	cmd.AddCommand(NewPushCmd())
	cmd.AddCommand(NewPullCmd())
	cmd.AddCommand(NewReadmeCmd())
	cmd.AddCommand(NewDebugCmd())
	cmd.AddCommand(NewFeedbackCmd())
	cmd.AddCommand(NewCatalogCmd())

	return cmd
}

func Execute() {
	// Set the version for automatic telemetry initialization
	telemetry.SetGlobalTelemetryVersion(Version)

	// Print startup message only on first installation/setup
	if isFirstRun() {
		startupMsg := fmt.Sprintf(`
Welcome to cagent! 🚀

For any feedback, please visit: %s

We collect anonymous usage data to help improve cagent. To disable:
  - Set environment variable: TELEMETRY_ENABLED=false

`, FeedbackLink)
		_, _ = os.Stdout.WriteString(startupMsg)
	}

	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
