package server

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/session"
)

type Server struct {
	e  *echo.Echo
	sm *SessionManager
}

func New(ctx context.Context, sessionStore session.Store, runConfig *config.RuntimeConfig, refreshInterval time.Duration, agentSources config.Sources) (*Server, error) {
	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middleware.RequestLogger())

	s := &Server{
		e:  e,
		sm: NewSessionManager(ctx, agentSources, sessionStore, refreshInterval, runConfig),
	}

	group := e.Group("/api")

	// List all available agents
	group.GET("/agents", s.getAgents)
	// Get an agent by id
	group.GET("/agents/:id", s.getAgentConfig)

	// List all sessions
	group.GET("/sessions", s.getSessions)
	// Get a session by id
	group.GET("/sessions/:id", s.getSession)
	// Resume a session by id
	group.POST("/sessions/:id/resume", s.resumeSession)
	// Toggle YOLO mode for a session
	group.POST("/sessions/:id/tools/toggle", s.toggleSessionYolo)
	// Toggle thinking mode for a session
	group.POST("/sessions/:id/thinking/toggle", s.toggleSessionThinking)
	// Update session permissions
	group.PATCH("/sessions/:id/permissions", s.updateSessionPermissions)
	// Update session title
	group.PATCH("/sessions/:id/title", s.updateSessionTitle)
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
	agents := []api.Agent{}
	for k, agentSource := range s.sm.Sources {
		slog.Debug("API source", "source", agentSource.Name())

		c, err := config.Load(c.Request().Context(), agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		desc := c.Agents.First().Description

		switch {
		case len(c.Agents) > 1:
			agents = append(agents, api.Agent{
				Name:        k,
				Multi:       true,
				Description: desc,
			})
		case len(c.Agents) == 1:
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

	slices.SortFunc(agents, func(a, b api.Agent) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return c.JSON(http.StatusOK, agents)
}

func (s *Server) getAgentConfig(c echo.Context) error {
	agentID := c.Param("id")

	for k, agentSource := range s.sm.Sources {
		if k != agentID {
			continue
		}

		slog.Debug("API source", "source", agentSource.Name())
		cfg, err := config.Load(c.Request().Context(), agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		return c.JSON(http.StatusOK, cfg)
	}

	return echo.NewHTTPError(http.StatusNotFound)
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

	return c.JSON(http.StatusOK, api.SessionResponse{
		ID:            sess.ID,
		Title:         sess.Title,
		CreatedAt:     sess.CreatedAt,
		Messages:      sess.GetAllMessages(),
		ToolsApproved: sess.ToolsApproved,
		Thinking:      sess.Thinking,
		InputTokens:   sess.InputTokens,
		OutputTokens:  sess.OutputTokens,
		WorkingDir:    sess.WorkingDir,
		Permissions:   sess.Permissions,
	})
}

func (s *Server) resumeSession(c echo.Context) error {
	var req api.ResumeSessionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.ResumeSession(c.Request().Context(), c.Param("id"), req.Confirmation, req.Reason); err != nil {
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

func (s *Server) toggleSessionThinking(c echo.Context) error {
	if err := s.sm.ToggleThinking(c.Request().Context(), c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to toggle session thinking mode: %v", err))
	}
	return c.JSON(http.StatusOK, nil)
}

func (s *Server) updateSessionPermissions(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.UpdateSessionPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.UpdateSessionPermissions(c.Request().Context(), sessionID, req.Permissions); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to update session permissions: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session permissions updated"})
}

func (s *Server) updateSessionTitle(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.UpdateSessionTitleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.UpdateSessionTitle(c.Request().Context(), sessionID, req.Title); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to update session title: %v", err))
	}

	return c.JSON(http.StatusOK, api.UpdateSessionTitleResponse{
		ID:    sessionID,
		Title: req.Title,
	})
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
	currentAgent := cmp.Or(c.Param("agent_name"), "root")

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
