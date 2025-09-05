package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/cagent/internal/config"
	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/internal/version"
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
	}

	// Add persistent debug flag available to all commands
	cmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVarP(&enableOtel, "otel", "o", false, "Enable OpenTelemetry tracing")
	cmd.PersistentFlags().StringVar(&logFilePath, "log-file", "", "Path to log file (default: ~/.cagent/cagent.log)")

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// setupLogging configures slog to write to a file instead of stdout/stderr when using the TUI.
// By default, it writes to <dataDir>/logs/cagent-<timestamp>.log. Users can override with --log-file.
func setupLogging(cmd *cobra.Command) error {
	// Determine log file path
	if logFilePath != "" {
		path := logFilePath
		if path == "" {
			dataDir := config.GetDataDir()
			path = filepath.Join(dataDir, "cagent.log")
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

		// Configure slog default logger
		level := slog.LevelInfo
		if debugMode {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})))
		return nil
	}

	// Else, decide based on TUI flag: file when TUI is enabled, stderr otherwise
	useTUI := false
	if cmd != nil && cmd.Name() == "run" {
		if f := cmd.Flags().Lookup("tui"); f != nil {
			if v, err := cmd.Flags().GetBool("tui"); err == nil {
				useTUI = v
			}
		}
	}
	if useTUI {
		dataDir := config.GetDataDir()
		logsDir := filepath.Join(dataDir, "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			return err
		}
		timestamp := time.Now().Format("20060102-150405")
		path := filepath.Join(logsDir, fmt.Sprintf("cagent-%s.log", timestamp))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		logFile = f
		level := slog.LevelInfo
		if debugMode {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})))
		return nil
	}

	// Default to stderr
	level := slog.LevelInfo
	if debugMode {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	return nil
}
