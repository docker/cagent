package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/session"
)

type Server struct {
	e  *echo.Echo
	sm *sessionManager
}

func New(ctx context.Context, sessionStore session.Store, runConfig *config.RuntimeConfig, refreshInterval time.Duration, agentSources config.Sources) (*Server, error) {
	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())

	s := &Server{
		e:  e,
		sm: newSessionManager(ctx, agentSources, sessionStore, refreshInterval, runConfig),
	}

	group := e.Group("/api")

	// List all available agents or get a specific agent by ID
	group.GET("/agents/*", s.getAgents)

	// List all sessions
	group.GET("/sessions", s.getSessions)
	// Get a session by id
	group.GET("/sessions/:id", s.getSession)
	// Resume a session by id
	group.POST("/sessions/:id/resume", s.resumeSession)
	// Toggle YOLO mode for a session
	group.POST("/sessions/:id/tools/toggle", s.toggleSessionYolo)
	// Create a new session
	group.POST("/sessions", s.createSession)
	// Delete a session
	group.DELETE("/sessions/:id", s.deleteSession)
	// Run an agent loop
	group.POST("/sessions/:id/agent/:agent", s.runAgent)
	group.POST("/sessions/:id/agent/:agent/:agent_name", s.runAgent)
	group.POST("/sessions/:id/elicitation", s.elicitation)

	// Health check endpoint
	group.GET("/ping", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	return s, nil
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := http.Server{
		Handler: s.e,
	}

	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		slog.Error("Failed to start server", "error", err)
		return err
	}

	return nil
}

func (s *Server) getAgents(c echo.Context) error {
	// Parse wildcard parameter
	wildcardPath := c.Param("*")

	// If wildcard is empty or just "/", list all agents
	if wildcardPath == "" || wildcardPath == "/" {
		agents := []api.Agent{}
		for k, agentSource := range s.sm.sources {
			slog.Debug("API source", "source", agentSource.Name())

			cfg, err := config.Load(c.Request().Context(), agentSource)
			if err != nil {
				slog.Error("Failed to load config from API source", "key", k, "error", err)
				continue
			}

			var desc string
			if a, ok := cfg.Agents["root"]; ok {
				desc = a.Description
			} else {
				for _, agent := range cfg.Agents {
					desc = agent.Description
					break
				}
			}
			switch {
			case len(cfg.Agents) > 1:
				agents = append(agents, api.Agent{
					Name:        k,
					Multi:       true,
					Description: desc,
				})
			case len(cfg.Agents) == 1:
				agents = append(agents, api.Agent{
					Name:        k,
					Multi:       false,
					Description: desc,
				})
			default:
				slog.Warn("No agents found in config from API source", "key", k)
				continue
			}
		}

		sort.Slice(agents, func(i, j int) bool {
			return agents[i].Name < agents[j].Name
		})

		return c.JSON(http.StatusOK, agents)
	}

	// get specific agent by ID
	tryFindAgent := func(path string) (config.Source, string) {
		// Try as-is
		if src, found := s.sm.sources[path]; found {
			return src, path
		}
		// Try with leading slash
		withSlash := "/" + path
		if src, found := s.sm.sources[withSlash]; found {
			return src, withSlash
		}
		// Try without leading slash
		withoutSlash := ""
		if len(path) > 0 && path[0] == '/' {
			withoutSlash = path[1:]
		}
		if withoutSlash != "" {
			if src, found := s.sm.sources[withoutSlash]; found {
				return src, withoutSlash
			}
		}
		// Try matching by name or basename
		for k, source := range s.sm.sources {
			if k == path || source.Name() == path || filepath.Base(k) == filepath.Base(path) || filepath.Base(source.Name()) == filepath.Base(path) {
				return source, k
			}
		}
		return nil, ""
	}

	src, id := tryFindAgent(wildcardPath)
	cfg, err := config.Load(c.Request().Context(), src)
	if err != nil {
		slog.Error("Failed to load agent", "key", id, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to load agent: %v", err))
	}

	return c.JSON(http.StatusOK, cfg)
}

func (s *Server) getSessions(c echo.Context) error {
	sessions, err := s.sm.GetSessions(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get sessions: %v", err))
	}

	responses := make([]api.SessionsResponse, len(sessions))
	for i, sess := range sessions {
		responses[i] = api.SessionsResponse{
			ID:           sess.ID,
			Title:        sess.Title,
			CreatedAt:    sess.CreatedAt.Format(time.RFC3339),
			NumMessages:  len(sess.GetAllMessages()),
			InputTokens:  sess.InputTokens,
			OutputTokens: sess.OutputTokens,
			WorkingDir:   sess.WorkingDir,
		}
	}
	return c.JSON(http.StatusOK, responses)
}

func (s *Server) createSession(c echo.Context) error {
	var sessionTemplate session.Session
	if err := c.Bind(&sessionTemplate); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	sess, err := s.sm.CreateSession(c.Request().Context(), &sessionTemplate)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create session: %v", err))
	}

	return c.JSON(http.StatusOK, sess)
}

func (s *Server) getSession(c echo.Context) error {
	sess, err := s.sm.GetSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	sr := api.SessionResponse{
		ID:            sess.ID,
		Title:         sess.Title,
		CreatedAt:     sess.CreatedAt,
		Messages:      sess.GetAllMessages(),
		ToolsApproved: sess.ToolsApproved,
		InputTokens:   sess.InputTokens,
		OutputTokens:  sess.OutputTokens,
		WorkingDir:    sess.WorkingDir,
	}

	return c.JSON(http.StatusOK, sr)
}

func (s *Server) resumeSession(c echo.Context) error {
	var req api.ResumeSessionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.ResumeSession(c.Request().Context(), c.Param("id"), req.Confirmation); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to resume session: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session resumed"})
}

func (s *Server) toggleSessionYolo(c echo.Context) error {
	if err := s.sm.ToggleToolApproval(c.Request().Context(), c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to toggle session tool approval mode: %v", err))
	}
	return c.JSON(http.StatusOK, nil)
}

func (s *Server) deleteSession(c echo.Context) error {
	sessionID := c.Param("id")

	if err := s.sm.DeleteSession(c.Request().Context(), sessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to delete session: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session deleted"})
}

func (s *Server) runAgent(c echo.Context) error {
	sessionID := c.Param("id")
	agentFilename := c.Param("agent")
	currentAgent := c.Param("agent_name")
	if currentAgent == "" {
		currentAgent = "root"
	}

	slog.Debug("Running agent", "agent_filename", agentFilename, "session_id", sessionID, "current_agent", currentAgent)

	var messages []api.Message
	if err := json.NewDecoder(c.Request().Body).Decode(&messages); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	streamChan, err := s.sm.RunSession(c.Request().Context(), sessionID, agentFilename, currentAgent, messages)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to run session: %v", err))
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)
	for event := range streamChan {
		data, err := json.Marshal(event)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal event: %v", err))
		}
		fmt.Fprintf(c.Response(), "data: %s\n\n", string(data))
		c.Response().Flush()
	}

	return nil
}

func (s *Server) elicitation(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeElicitationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.ResumeElicitation(c.Request().Context(), sessionID, req.Action, req.Content); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to resume elicitation: %v", err))
	}

	return c.JSON(http.StatusOK, nil)
}
