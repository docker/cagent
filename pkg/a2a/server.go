package a2a

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	"google.golang.org/adk/session"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/version"
)

// routableAddr replaces wildcard listen addresses (like "0.0.0.0" or "::") with
// "localhost" so the agent card URL is actually usable by clients.
func routableAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return net.JoinHostPort("localhost", port)
	}
	return addr
}

func Run(ctx context.Context, agentFilename, agentName string, runConfig *config.RuntimeConfig, ln net.Listener) error {
	slog.Debug("Starting A2A server", "agent", agentName, "addr", ln.Addr().String())

	agentSource, err := config.Resolve(agentFilename, nil)
	if err != nil {
		return err
	}

	t, err := teamloader.Load(ctx, agentSource, runConfig)
	if err != nil {
		return fmt.Errorf("failed to load agents: %w", err)
	}
	defer func() {
		if err := t.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}()

	adkAgent, err := newCAgentAdapter(t, agentName)
	if err != nil {
		return fmt.Errorf("failed to create ADK agent adapter: %w", err)
	}

	baseURL := &url.URL{Scheme: "http", Host: routableAddr(ln.Addr().String())}

	slog.Debug("A2A server listening", "url", baseURL.String())

	name := strings.TrimSuffix(filepath.Base(agentFilename), filepath.Ext(agentFilename))

	agentPath := "/invoke"
	agentCard := &a2a.AgentCard{
		Name:        name,
		Description: adkAgent.Description(),
		Skills: []a2a.AgentSkill{{
			ID:          fmt.Sprintf("%s_%s", name, agentName),
			Name:        agentName,
			Description: adkAgent.Description(),
			Tags:        []string{"llm", "cagent"},
		}},
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		URL:                baseURL.JoinPath(agentPath).String(),
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Version:            version.Version,
		DefaultInputModes:  []string{},
		DefaultOutputModes: []string{},
	}

	executor := newExecutorWrapper(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        name,
			Agent:          adkAgent,
			SessionService: session.InMemoryService(),
		},
	})

	// Start server
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Content-Type", "Accept"},
		MaxAge:       86400,
	}))
	e.Use(middleware.RequestLogger())

	e.GET(a2asrv.WellKnownAgentCardPath, echo.WrapHandler(a2asrv.NewStaticAgentCardHandler(agentCard)))
	e.POST(agentPath, echo.WrapHandler(a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))))

	if err := e.Server.Serve(ln); err != nil && ctx.Err() == nil {
		slog.Error("Failed to start server", "error", err)
		return err
	}

	return nil
}
