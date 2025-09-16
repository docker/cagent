package root

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
	"github.com/spf13/cobra"
)

var (
	agentName   string
	debugMode   bool
	enableOtel  bool
	logFilePath string
	logFile     *os.File
)

// isFirstRun checks if this is the first time cagent is being run
// It creates a marker file in the user's config directory
func isFirstRun() bool {
	configDir := paths.GetConfigDir()
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
			// Initialize logging before anything else so logs don't break TUI
			if err := setupLogging(cmd); err != nil {
				// If logging setup fails, fall back to stderr so we still get logs
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: func() slog.Level {
						if debugMode {
							return slog.LevelDebug
						}
						return slog.LevelInfo
					}(),
				})))
			}
			if cmd.DisplayName() != "exec" && os.Getenv("CAGENT_HIDE_FEEDBACK_LINK") != "1" {
				_, _ = cmd.OutOrStdout().Write([]byte("\nFor any feedback, please visit: " + FeedbackLink + "\n\n"))
			}

			telemetry.SetGlobalTelemetryDebugMode(debugMode)
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if logFile != nil {
				_ = logFile.Close()
			}
			return nil
		},
		// If no subcommand is specified, show help
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Add persistent debug flag available to all commands
	cmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVarP(&enableOtel, "otel", "o", false, "Enable OpenTelemetry tracing")
	cmd.PersistentFlags().StringVar(&logFilePath, "log-file", "", "Path to debug log file (default: ~/.cagent/cagent.debug.log; only used with --debug)")

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
	cmd.AddCommand(NewBuildCmd())

	return cmd
}

func Run() {
	Execute()
}

func Execute() {
	// Set the version for automatic telemetry initialization
	telemetry.SetGlobalTelemetryVersion(version.Version)

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
		envErr := &environment.RequiredEnvError{}
		if errors.As(err, &envErr) {
			fmt.Fprintln(os.Stderr, "The following environment variables must be set:")
			for _, v := range envErr.Missing {
				fmt.Fprintf(os.Stderr, " - %s\n", v)
			}
			fmt.Fprintln(os.Stderr, "\nEither:\n - Set those environment variables before running cagent\n - Run cagent with --env-from-file\n - Store those secrets using one of the built-in environment variable providers.")
		} else {
			fmt.Fprintln(os.Stderr, err)
			_ = rootCmd.Usage()
		}

		os.Exit(1)
	}
}

// setupLogging configures slog logging behavior.
// When --debug is enabled, logs are written to a single file <dataDir>/cagent.debug.log (append mode),
// or to the file specified by --log-file. When in the TUI, structured logs are suppressed if not in --debug mode
func setupLogging(cmd *cobra.Command) error {
	level := slog.LevelInfo
	if debugMode {
		level = slog.LevelDebug
	}

	// Determine if TUI is enabled for the run command
	useTUI := false
	if cmd != nil && cmd.Name() == "run" {
		if f := cmd.Flags().Lookup("tui"); f != nil {
			if v, err := cmd.Flags().GetBool("tui"); err == nil {
				useTUI = v
			}
		}
	}

	var writer io.Writer
	if debugMode {
		// Determine path from flag or default to <dataDir>/cagent.debug.log
		path := strings.TrimSpace(logFilePath)
		if path == "" {
			dataDir := paths.GetDataDir()
			path = filepath.Join(dataDir, "cagent.debug.log")
		} else {
			if path == "~" || strings.HasPrefix(path, "~/") {
				homeDir, err := os.UserHomeDir()
				if err == nil {
					path = filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
				}
			} else if strings.HasPrefix(path, "~\\") { // Windows-style path expansion
				homeDir, err := os.UserHomeDir()
				if err == nil {
					path = filepath.Join(homeDir, strings.TrimPrefix(path, "~\\"))
				}
			}
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		// Open file for appending
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		logFile = f

		// In debug mode, write to file; mirror to stderr when not in TUI
		if useTUI {
			writer = f
		} else {
			writer = io.MultiWriter(f, os.Stderr)
		}
	} else {
		// Non-debug: discard logs in TUI to keep interface clean, else stderr
		if useTUI {
			writer = io.Discard
		} else {
			writer = os.Stderr
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level})))
	return nil
}
