package root

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/logging"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/version"
)

type rootFlags struct {
	enableOtel  bool
	debugMode   bool
	logFilePath string
	logFile     io.Closer
}

func isCliPLugin() bool {
	cliPluginBinary := "docker-agent"
	if runtime.GOOS == "windows" {
		cliPluginBinary += ".exe"
	}
	return len(os.Args) > 0 && strings.HasSuffix(os.Args[0], cliPluginBinary)
}

func NewRootCmd() *cobra.Command {
	var flags rootFlags

	cmd := &cobra.Command{
		Use:   "cagent",
		Short: "cagent - AI agent runner",
		Long:  "cagent is a command-line tool for running AI agents",
		Example: `  cagent run
  cagent run ./agent.yaml
  cagent run agentcatalog/pirate`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging before anything else so logs don't break TUI
			if err := flags.setupLogging(); err != nil {
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
				if err := flags.logFile.Close(); err != nil {
					slog.Error("Failed to close log file", "error", err)
				}
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
	cmd.AddCommand(newNewCmd())
	cmd.AddCommand(newEvalCmd())
	cmd.AddCommand(newShareCmd())
	cmd.AddCommand(newDebugCmd())
	cmd.AddCommand(newAliasCmd())
	cmd.AddCommand(newServeCmd())

	// Define groups
	cmd.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"})
	cmd.AddGroup(&cobra.Group{ID: "advanced", Title: "Advanced Commands:"})

	if isCliPLugin() {
		cmd.Use = "agent"
		cmd.Short = "create or run AI agents"
		cmd.Long = "create or run AI agents"
		cmd.Example = `  docker agent run ./agent.yaml
  docker agent run agentcatalog/pirate`
	}

	return cmd
}

func Execute(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	// Set the version for automatic telemetry initialization
	telemetry.SetGlobalTelemetryVersion(version.Version)

	// Print startup message only on first installation/setup
	if isFirstRun() && os.Getenv("CAGENT_HIDE_TELEMETRY_BANNER") != "1" {
		welcomeMsg := fmt.Sprintf(`
Welcome to cagent! 🚀

For any feedback, please visit: %s
`, feedback.Link)
		fmt.Fprint(stderr, welcomeMsg)

		// Only show telemetry notice when telemetry is enabled
		if telemetry.GetTelemetryEnabled() {
			telemetryMsg := `
We collect anonymous usage data to help improve cagent. To disable:
  - Set environment variable: TELEMETRY_ENABLED=false
`
			fmt.Fprint(stderr, telemetryMsg)
		}

		fmt.Fprintln(stderr)
	}

	rootCmd := NewRootCmd()

	// When no subcommand is given, default to "run" (which runs the default agent).
	// Users can use "cagent --help" to see available commands.
	args = defaultToRun(rootCmd, args)

	rootCmd.SetArgs(args)
	rootCmd.SetIn(stdin)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	if isCliPLugin() {
		plugin.Run(func(dockerCli command.Cli) *cobra.Command {
			originalPreRun := rootCmd.PersistentPreRunE
			rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
				if err := plugin.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
				if originalPreRun != nil {
					if err := originalPreRun(cmd, args); err != nil {
						return processErr(ctx, err, stderr, rootCmd)
					}
				}
				return nil
			}
			rootCmd.SetContext(ctx)
			return rootCmd
		}, metadata.Metadata{
			SchemaVersion: "0.1.0",
			Vendor:        "Docker Inc.",
			Version:       version.Version,
		})
	} else if err := rootCmd.ExecuteContext(ctx); err != nil {
		return processErr(ctx, err, stderr, rootCmd)
	}

	return nil
}

// defaultToRun prepends "run" to the argument list when no subcommand is
// specified so that bare "cagent" (or "cagent --debug", etc.) launches the
// default agent. Help flags (--help / -h) are left alone.
func defaultToRun(rootCmd *cobra.Command, args []string) []string {
	for _, arg := range args {
		switch {
		case arg == "--":
			// End of flags – no subcommand found.
			return append([]string{"run"}, args...)
		case arg == "--help" || arg == "-h":
			return args
		case strings.HasPrefix(arg, "-"):
			continue
		case isSubcommand(rootCmd, arg):
			return args
		default:
			return append([]string{"run"}, args...)
		}
	}

	return append([]string{"run"}, args...)
}

// isSubcommand reports whether name matches a registered subcommand or alias.
func isSubcommand(cmd *cobra.Command, name string) bool {
	if name == "help" || name == "completion" {
		return true
	}
	for _, sub := range cmd.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return true
		}
	}
	return false
}

func processErr(ctx context.Context, err error, stderr io.Writer, rootCmd *cobra.Command) error {
	if ctx.Err() != nil {
		return ctx.Err()
	} else if envErr, ok := errors.AsType[*environment.RequiredEnvError](err); ok {
		fmt.Fprintln(stderr, "The following environment variables must be set:")
		for _, v := range envErr.Missing {
			fmt.Fprintf(stderr, " - %s\n", v)
		}
		fmt.Fprintln(stderr, "\nEither:\n - Set those environment variables before running cagent\n - Run cagent with --env-from-file\n - Store those secrets using one of the built-in environment variable providers.")
	} else if _, ok := errors.AsType[RuntimeError](err); ok {
		// Runtime errors have already been printed by the command itself
		// Don't print them again or show usage
	} else {
		// Command line usage errors - show the error and usage
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr)
		if strings.HasPrefix(err.Error(), "unknown command ") || strings.HasPrefix(err.Error(), "accepts ") {
			_ = rootCmd.Usage()
		}
	}

	return err
}

// setupLogging configures slog logging behavior.
// When --debug is enabled, logs are written to a rotating file <dataDir>/cagent.debug.log,
// or to the file specified by --log-file. Log files are rotated when they exceed 10MB,
// keeping up to 3 backup files.
func (f *rootFlags) setupLogging() error {
	if !f.debugMode {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return nil
	}

	path := cmp.Or(strings.TrimSpace(f.logFilePath), filepath.Join(paths.GetDataDir(), "cagent.debug.log"))

	logFile, err := logging.NewRotatingFile(path)
	if err != nil {
		return err
	}
	f.logFile = logFile

	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})))

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

// isFirstRun checks if this is the first time cagent is being run.
// It atomically creates a marker file in the user's config directory
// using os.O_EXCL to avoid a race condition when multiple processes
// start concurrently.
func isFirstRun() bool {
	configDir := paths.GetConfigDir()
	markerFile := filepath.Join(configDir, ".cagent_first_run")

	// Ensure the config directory exists before trying to create the marker file
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		slog.Warn("Failed to create config directory for first run marker", "error", err)
		return false
	}

	// Atomically create the marker file. If it already exists, OpenFile returns an error.
	f, err := os.OpenFile(markerFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false // File already exists or other error, not first run
	}
	f.Close()

	return true
}
