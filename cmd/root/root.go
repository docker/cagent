package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

type rootFlags struct {
	enableOtel  bool
	debugMode   bool
	logFilePath string
	logFile     *os.File
}

func NewRootCmd() *cobra.Command {
	var flags rootFlags

	cmd := &cobra.Command{
		Use:   "cagent",
		Short: "cagent - AI agent runner",
		Long:  `cagent is a command-line tool for running AI agents`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging before anything else so logs don't break TUI
			if err := flags.setupLogging(cmd); err != nil {
				// If logging setup fails, fall back to stderr so we still get logs
				slog.SetDefault(slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
					Level: func() slog.Level {
						if flags.debugMode {
							return slog.LevelDebug
						}
						return slog.LevelInfo
					}(),
				})))
			}

			telemetry.SetGlobalTelemetryDebugMode(flags.debugMode)

			if flags.enableOtel {
				if err := initOTelSDK(cmd.Context()); err != nil {
					slog.Warn("Failed to initialize OpenTelemetry SDK", "error", err)
				} else {
					slog.Debug("OpenTelemetry SDK initialized successfully")
				}
			}

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if flags.logFile != nil {
				_ = flags.logFile.Close()
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
	cmd.PersistentFlags().BoolVarP(&flags.debugMode, "debug", "d", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVarP(&flags.enableOtel, "otel", "o", false, "Enable OpenTelemetry tracing")
	cmd.PersistentFlags().StringVar(&flags.logFilePath, "log-file", "", "Path to debug log file (default: ~/.cagent/cagent.debug.log; only used with --debug)")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newNewCmd())
	cmd.AddCommand(newAPICmd())
	cmd.AddCommand(newACPCmd())
	cmd.AddCommand(newMCPCmd())
	cmd.AddCommand(newEvalCmd())
	cmd.AddCommand(newPushCmd())
	cmd.AddCommand(newPullCmd())
	cmd.AddCommand(newDebugCmd())
	cmd.AddCommand(newFeedbackCmd())
	cmd.AddCommand(newCatalogCmd())
	cmd.AddCommand(newBuildCmd())
	cmd.AddCommand(newPrintCmd())
	cmd.AddCommand(newAliasCmd())

	return cmd
}

func Execute(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	// Set the version for automatic telemetry initialization
	telemetry.SetGlobalTelemetryVersion(version.Version)

	// Print startup message only on first installation/setup
	if isFirstRun() && os.Getenv("CAGENT_HIDE_TELEMETRY_BANNER") != "1" {
		startupMsg := fmt.Sprintf(`
Welcome to cagent! 🚀

For any feedback, please visit: %s

We collect anonymous usage data to help improve cagent. To disable:
  - Set environment variable: TELEMETRY_ENABLED=false

`, feedback.FeedbackLink)
		fmt.Fprint(stderr, startupMsg)
	}

	rootCmd := NewRootCmd()
	rootCmd.SetArgs(args)
	rootCmd.SetIn(stdin)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		envErr := &environment.RequiredEnvError{}
		runtimeErr := RuntimeError{}

		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &envErr):
			fmt.Fprintln(stderr, "The following environment variables must be set:")
			for _, v := range envErr.Missing {
				fmt.Fprintf(stderr, " - %s\n", v)
			}
			fmt.Fprintln(stderr, "\nEither:\n - Set those environment variables before running cagent\n - Run cagent with --env-from-file\n - Store those secrets using one of the built-in environment variable providers.")
		case errors.As(err, &runtimeErr):
			// Runtime errors have already been printed by the command itself
			// Don't print them again or show usage
		default:
			// Command line usage errors - show the error and usage
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr)
			if strings.HasPrefix(err.Error(), "unknown command ") || strings.HasPrefix(err.Error(), "accepts ") {
				_ = rootCmd.Usage()
			}
		}

		return err
	}

	return nil
}

// setupLogging configures slog logging behavior.
// When --debug is enabled, logs are written to a single file <dataDir>/cagent.debug.log (append mode),
// or to the file specified by --log-file. When in the TUI, structured logs are suppressed if not in --debug mode
func (f *rootFlags) setupLogging(cmd *cobra.Command) error {
	level := slog.LevelInfo
	if f.debugMode {
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
	if f.debugMode {
		// Determine path from flag or default to <dataDir>/cagent.debug.log
		path := strings.TrimSpace(f.logFilePath)
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
		logFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		f.logFile = logFile

		// In debug mode, write to file; mirror to stderr when not in TUI
		if useTUI {
			writer = logFile
		} else {
			writer = io.MultiWriter(logFile, os.Stderr)
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

// RuntimeError wraps runtime errors to distinguish them from usage errors
type RuntimeError struct {
	Err error
}

func (e RuntimeError) Error() string {
	return e.Err.Error()
}

func (e RuntimeError) Unwrap() error {
	return e.Err
}

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
