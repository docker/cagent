package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

type apiFlags struct {
	listenAddr       string
	sessionDB        string
	pullIntervalMins int
	runConfig        config.RuntimeConfig
}

func newAPICmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "api <agent-file>|<agents-dir>",
		Short: "Start the API server",
		Long:  `Start the API server that exposes the agent via an HTTP API`,
		Args:  cobra.ExactArgs(1),
		RunE:  flags.runAPICommand,
	}

	cmd.PersistentFlags().StringVarP(&flags.listenAddr, "listen", "l", ":8080", "Address to listen on")
	cmd.PersistentFlags().StringVarP(&flags.sessionDB, "session-db", "s", "session.db", "Path to the session database")
	cmd.PersistentFlags().IntVar(&flags.pullIntervalMins, "pull-interval", 0, "Auto-pull OCI reference every N minutes (0 = disabled)")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

// isOCIReference checks if the input is a valid OCI reference
func isOCIReference(input string) bool {
	if agentfile.IsLocalFile(input) {
		return false
	}
	_, err := name.ParseReference(input)
	return err == nil
}

func (f *apiFlags) runAPICommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("api", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())
	agentsPath := args[0]

	// Make sure no question is ever asked to the user in api mode.
	os.Stdin = nil

	if f.pullIntervalMins > 0 && !isOCIReference(agentsPath) {
		return fmt.Errorf("--pull-interval flag can only be used with OCI references, not local files")
	}

	resolvedPath, err := agentfile.Resolve(ctx, out, agentsPath)
	if err != nil {
		return err
	}

	ln, err := server.Listen(ctx, f.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", f.listenAddr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	slog.Info("Listening on " + ln.Addr().String())

	slog.Debug("Starting server", "agents", resolvedPath)

	sessionStore, err := session.NewSQLiteSessionStore(f.sessionDB)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}

	var opts []server.Opt

	stat, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to stat agents path: %w", err)
	}
	if stat.IsDir() {
		opts = append(opts, server.WithAgentsDir(resolvedPath))
	} else {
		opts = append(opts, server.WithAgentsDir(filepath.Dir(resolvedPath)))
	}

	teams, err := teamloader.LoadTeams(ctx, resolvedPath, f.runConfig)
	if err != nil {
		return fmt.Errorf("failed to load teams: %w", err)
	}
	defer func() {
		for _, team := range teams {
			if err := team.StopToolSets(ctx); err != nil {
				slog.Error("Failed to stop tool sets", "error", err)
			}
		}
	}()

	s, err := server.New(sessionStore, f.runConfig, teams, opts...)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start background auto-pull for OCI references if enabled
	if f.pullIntervalMins > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(f.pullIntervalMins) * time.Minute)
			defer ticker.Stop()

			slog.Info("Auto-pull enabled for OCI reference", "reference", agentsPath, "interval_minutes", f.pullIntervalMins)

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					slog.Info("Auto-pulling OCI reference", "reference", agentsPath)
					if _, err := remote.Pull(agentsPath); err != nil {
						slog.Error("Failed to auto-pull OCI reference", "reference", agentsPath, "error", err)
						continue
					}

					// Resolve the OCI reference to get the updated file path
					newResolvedPath, err := agentfile.Resolve(ctx, out, agentsPath)
					if err != nil {
						slog.Error("Failed to resolve OCI reference after pull", "reference", agentsPath, "error", err)
						continue
					}

					if err := s.ReloadTeams(ctx, newResolvedPath); err != nil {
						slog.Error("Failed to reload teams", "reference", agentsPath, "error", err)
					} else {
						slog.Info("Successfully reloaded teams from updated OCI reference", "reference", agentsPath)
					}
				}
			}
		}()
	}

	return s.Serve(ctx, ln)
}
