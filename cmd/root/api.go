package root

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/connectrpc"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
)

type apiFlags struct {
	listenAddr       string
	sessionDB        string
	pullIntervalMins int
	fakeResponses    string
	recordPath       string
	connectRPC       bool
	exitOnStdinEOF   bool
	runConfig        config.RuntimeConfig
}

func newAPICmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:     "api <agent-file>|<agents-dir>",
		Short:   "Start the cagent API server",
		Long:    `Start the API server that exposes the agent via a cagent-specific HTTP API`,
		GroupID: "server",
		Args:    cobra.ExactArgs(1),
		RunE:    flags.runAPICommand,
	}

	cmd.PersistentFlags().StringVarP(&flags.listenAddr, "listen", "l", ":8080", "Address to listen on")
	cmd.PersistentFlags().StringVarP(&flags.sessionDB, "session-db", "s", "session.db", "Path to the session database")
	cmd.PersistentFlags().IntVar(&flags.pullIntervalMins, "pull-interval", 0, "Auto-pull OCI reference every N minutes (0 = disabled)")
	cmd.PersistentFlags().StringVar(&flags.fakeResponses, "fake", "", "Replay AI responses from cassette file (for testing)")
	cmd.PersistentFlags().StringVar(&flags.recordPath, "record", "", "Record AI API interactions to cassette file")
	cmd.PersistentFlags().BoolVar(&flags.connectRPC, "connect-rpc", false, "Use Connect-RPC protocol instead of HTTP/JSON API")
	cmd.PersistentFlags().BoolVar(&flags.exitOnStdinEOF, "exit-on-stdin-eof", false, "Exit when stdin is closed (for integration with parent processes)")
	_ = cmd.PersistentFlags().MarkHidden("exit-on-stdin-eof")
	cmd.MarkFlagsMutuallyExclusive("fake", "record")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

// monitorStdin monitors stdin for EOF, which indicates the parent process has died.
// When spawned with piped stdio, stdin closes when the parent process dies.
func monitorStdin(ctx context.Context, cancel context.CancelFunc, stdin *os.File) {
	// Close stdin when context is cancelled to unblock the read
	go func() {
		<-ctx.Done()
		stdin.Close()
	}()

	buf := make([]byte, 1)
	for {
		n, err := stdin.Read(buf)
		if err != nil || n == 0 {
			// Only log and cancel if context isn't already done (parent died)
			if ctx.Err() == nil {
				slog.Info("stdin closed, parent process likely died, shutting down")
				cancel()
			}
			return
		}
	}
}

func (f *apiFlags) runAPICommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("api", args)

	ctx := cmd.Context()

	// Save stdin before clearing it, so we can optionally monitor for parent process death
	stdin := os.Stdin

	out := cli.NewPrinter(cmd.OutOrStdout())
	agentsPath := args[0]

	// Make sure no question is ever asked to the user in api mode.
	os.Stdin = nil

	// Monitor stdin for EOF to detect parent process death.
	// Only enabled when --exit-on-stdin-eof flag is passed.
	// When spawned with piped stdio, stdin closes when the parent process dies.
	if f.exitOnStdinEOF && stdin != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go monitorStdin(ctx, cancel, stdin)
	}

	// Start fake proxy if --fake is specified
	cleanup, err := setupFakeProxy(f.fakeResponses, 0, &f.runConfig)
	if err != nil {
		return err
	}
	defer func() {
		if err := cleanup(); err != nil {
			slog.Error("Failed to cleanup fake proxy", "error", err)
		}
	}()

	// Start recording proxy if --record is specified
	if _, cleanup, err := setupRecordingProxy(f.recordPath, &f.runConfig); err != nil {
		return err
	} else if cleanup != nil {
		defer cleanup()
	}

	if f.pullIntervalMins > 0 && !config.IsOCIReference(agentsPath) {
		return fmt.Errorf("--pull-interval flag can only be used with OCI references, not local files")
	}

	ln, err := server.Listen(ctx, f.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", f.listenAddr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	out.Println("Listening on " + ln.Addr().String())

	slog.Debug("Starting server", "agents", agentsPath, "addr", ln.Addr().String())

	sessionStore, err := session.NewSQLiteSessionStore(f.sessionDB)
	if err != nil {
		return fmt.Errorf("creating session store: %w", err)
	}

	sources, err := config.ResolveSources(agentsPath)
	if err != nil {
		return fmt.Errorf("resolving agent sources: %w", err)
	}

	if f.connectRPC {
		s, err := connectrpc.New(ctx, sessionStore, &f.runConfig, time.Duration(f.pullIntervalMins)*time.Minute, sources)
		if err != nil {
			return fmt.Errorf("creating Connect-RPC server: %w", err)
		}
		return s.Serve(ctx, ln)
	}

	s, err := server.New(ctx, sessionStore, &f.runConfig, time.Duration(f.pullIntervalMins)*time.Minute, sources)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	return s.Serve(ctx, ln)
}
