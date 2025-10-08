package root

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

// NewApiCmd creates a new api command
func NewApiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api <agent-file>|<agents-dir>",
		Short: "Start the API server",
		Long:  `Start the API server that exposes the agent via an HTTP API`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("api", args)
			return runHttp(cmd, args)
		},
	}

	cmd.PersistentFlags().StringVarP(&listenAddr, "listen", "l", ":8080", "Address to listen on")
	cmd.PersistentFlags().StringVarP(&sessionDb, "session-db", "s", "session.db", "Path to the session database")
	addGatewayFlags(cmd)
	addRuntimeConfigFlags(cmd)

	return cmd
}

func runHttp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	agentsPath := args[0]

	ln, err := server.Listen(ctx, listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	if _, ok := ln.(*net.TCPListener); ok {
		slog.Info("Listening on http://localhost" + listenAddr)
	} else {
		slog.Info("Listening on " + listenAddr)
	}

	slog.Debug("Starting server", "agents", agentsPath, "debug_mode", debugMode)

	sessionStore, err := session.NewSQLiteSessionStore(sessionDb)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}

	var opts []server.Opt

	stat, err := os.Stat(agentsPath)
	if err != nil {
		return fmt.Errorf("failed to stat agents path: %w", err)
	}
	if stat.IsDir() {
		opts = append(opts, server.WithAgentsDir(agentsPath))
	} else {
		opts = append(opts, server.WithAgentsDir(filepath.Dir(agentsPath)))
	}

	teams, err := teamloader.LoadTeams(ctx, agentsPath, runConfig)
	if err != nil {
		return fmt.Errorf("failed to load teams: %w", err)
	}
	defer func() {
		for _, team := range teams {
			if err := team.StopToolSets(); err != nil {
				slog.Error("Failed to stop tool sets", "error", err)
			}
		}
	}()

	s, err := server.New(sessionStore, runConfig, teams, opts...)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	return s.Serve(ctx, ln)
}
