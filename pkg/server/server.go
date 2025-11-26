package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
)

type Server struct {
	e              *echo.Echo
	cancelsMu      sync.RWMutex
	sessionStore   session.Store
	runConfig      *config.RuntimeConfig
	teams          map[string]*team.Team
	teamsMu        sync.RWMutex
	runtimes       map[string]runtime.Runtime
	runtimeCancels map[string]context.CancelFunc
	ociRef         string // OCI reference, set once at startup for OCI-based servers
	ociTeamKey     string
	agentsDir      string
	agentsPath     string // For local files: specific file path to reload (instead of scanning agentsDir)
	rootFS         *os.Root
}

type Opt func(*Server) error

func WithAgentsDir(dir string) Opt {
	return func(s *Server) error {
		rootFs, err := os.OpenRoot(dir)
		if err != nil {
			return fmt.Errorf("failed to open root filesystem at %s: %w", dir, err)
		}

		s.agentsDir = dir
		s.rootFS = rootFs
		return nil
	}
}

func WithAgentsPath(agentPath string) Opt {
	return func(s *Server) error {
		s.agentsPath = agentPath
		return nil
	}
}

func WithOCIRef(teamKey, ociRef string) Opt {
	return func(s *Server) error {
		if teamKey == "" || ociRef == "" {
			return nil
		}

		s.ociTeamKey = teamKey
		s.ociRef = ociRef

		return nil
	}
}

func New(sessionStore session.Store, runConfig *config.RuntimeConfig, teams map[string]*team.Team, opts ...Opt) (*Server, error) {
	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())

	if teams == nil {
		teams = make(map[string]*team.Team)
	}

	s := &Server{
		e:              e,
		runtimes:       make(map[string]runtime.Runtime),
		runtimeCancels: make(map[string]context.CancelFunc),
		sessionStore:   sessionStore,
		runConfig:      runConfig,
		teams:          teams,
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	group := e.Group("/api")

	// List all available agents
	group.GET("/agents", s.getAgents)
	// Get an agent by id
	group.GET("/agents/:id", s.getAgentConfig)
	// Get an agent's raw YAML configuration by id
	group.GET("/agents/:id/yaml", s.getAgentConfigYAML)

	// SESSIONS

	// List all sessions
	group.GET("/sessions", s.getSessions)
	// Get sessions by agent filename
	group.GET("/sessions/agent/:id", s.getSessionsByAgent)
	// Get a session by id
	group.GET("/sessions/:id", s.getSession)
	// Resume a session by id
	group.POST("/sessions/:id/resume", s.resumeSession)
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

// hasAgentsDir returns true if the server has a local agents directory.
// Returns false for OCI reference-based servers, which load from memory.
func (s *Server) hasAgentsDir() bool {
	return s.agentsDir != "" && s.rootFS != nil
}

// getTeam retrieves a team by key with read lock
func (s *Server) getTeam(key string) (*team.Team, bool) {
	s.teamsMu.RLock()
	defer s.teamsMu.RUnlock()
	t, exists := s.teams[key]
	return t, exists
}

// getTeams returns a snapshot of all teams with read lock
func (s *Server) getTeams() map[string]*team.Team {
	s.teamsMu.RLock()
	defer s.teamsMu.RUnlock()
	teams := make(map[string]*team.Team, len(s.teams))
	for k, v := range s.teams {
		teams[k] = v
	}
	return teams
}

// replaceAllTeams replaces the entire teams map with write lock, returns old teams
func (s *Server) replaceAllTeams(teams map[string]*team.Team) map[string]*team.Team {
	s.teamsMu.Lock()
	defer s.teamsMu.Unlock()
	oldTeams := s.teams
	s.teams = teams
	return oldTeams
}

func (s *Server) getOCIRef(key string) (string, bool) {
	if s.ociRef == "" || s.ociTeamKey != key {
		return "", false
	}
	return s.ociRef, true
}

// API handlers

func (s *Server) getAgentConfig(c echo.Context) error {
	agentID := c.Param("id")
	p := addYamlExt(agentID)

	cfg, err := config.LoadConfig(c.Request().Context(), p, s.rootFS)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		return echo.NewHTTPError(http.StatusNotFound, "agent not found")
	}

	return c.JSON(http.StatusOK, cfg)
}

func (s *Server) getAgentConfigYAML(c echo.Context) error {
	agentID := c.Param("id")
	p := addYamlExt(agentID)

	// Read the YAML file
	yamlContent, err := s.rootFS.ReadFile(p)
	if err != nil {
		slog.Error("Failed to read agent YAML file", "path", p, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read agent file")
	}

	return c.String(http.StatusOK, string(yamlContent))
}

func (s *Server) getAgents(c echo.Context) error {
	// Refresh agents from disk to get the latest configurations
	// Only refresh for local files/dirs (hasAgentsDir == true)
	// OCI refs are immutable and loaded into memory, updates handled via ReloadTeams()
	if s.hasAgentsDir() {
		if err := s.refreshAgentsFromDisk(c.Request().Context()); err != nil {
			slog.Error("Failed to refresh agents from disk", "error", err)
		}
	}

	// DO NOT, under any circumstance, replace this with "var agents []api.Agent",
	// we want to return an empty slice if there are no agents, not nil.
	agents := []api.Agent{}

	// Manually lock the mutex to safely iterate over teams
	s.teamsMu.RLock()
	for id, t := range s.teams {
		a, err := t.Agent("root")
		if err != nil {
			switch {
			case t.Size() > 1:
				agents = append(agents, api.Agent{
					Name:  id,
					Multi: true,
				})
			case t.Size() == 1:
				a, err = t.Agent(t.AgentNames()[0])
				if err != nil {
					s.teamsMu.RUnlock()
					return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get agent for team %s: %v", id, err))
				}
				agents = append(agents, api.Agent{
					Name:        id,
					Description: a.Description(),
					Multi:       false,
				})
			default:
				slog.Warn("Team has no agents", "team", id)
				continue
			}
		} else {
			agents = append(agents, api.Agent{
				Name:        id,
				Description: a.Description(),
				Multi:       a.HasSubAgents(),
			})
		}
	}
	s.teamsMu.RUnlock()

	// Sort agents by name
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return c.JSON(http.StatusOK, agents)
}

func (s *Server) refreshAgentsFromDisk(ctx context.Context) error {
	// Use agentsPath if set (for specific files), otherwise fall back to agentsDir
	// Note: OCI references are handled via ReloadTeams() and never call this function
	// (see getAgents() which skips refresh for OCI refs)
	pathToLoad := s.agentsPath
	if pathToLoad == "" {
		pathToLoad = s.agentsDir
	}

	if pathToLoad == "" {
		return nil
	}

	slog.Debug("Refreshing agents from disk", "path", pathToLoad)

	newTeams, err := teamloader.LoadTeams(ctx, pathToLoad, s.runConfig)
	if err != nil {
		return fmt.Errorf("failed to load teams: %w", err)
	}

	oldTeams := s.getTeams()

	for id, oldTeam := range oldTeams {
		if _, exists := newTeams[id]; !exists {
			// Team no longer exists on disk, stop its tool sets
			slog.Info("Stopping tool sets for removed team", "team", id)
			if err := oldTeam.StopToolSets(ctx); err != nil {
				slog.Error("Failed to stop tool sets for removed team", "team", id, "error", err)
			}
		}
	}

	s.replaceAllTeams(newTeams)

	return nil
}

// ReloadTeams loads teams from the specified path and replaces the current teams.
// This method is thread-safe and ensures ongoing requests are not interrupted.
func (s *Server) ReloadTeams(ctx context.Context, agentPath string) error {
	// TODO(rumpl): this is completely broken and needs fixing, it is only reloading teams but it's IS NOT reloading runtimes.
	newTeams, err := teamloader.LoadTeams(ctx, agentPath, s.runConfig)
	if err != nil {
		return fmt.Errorf("failed to load teams: %w", err)
	}

	oldTeams := s.replaceAllTeams(newTeams)

	// Stop old teams' toolsets after releasing the lock to avoid blocking requests
	for id, oldTeam := range oldTeams {
		if err := oldTeam.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets for old team", "team", id, "error", err)
		}
	}

	return nil
}

func (s *Server) getSessions(c echo.Context) error {
	sessions, err := s.sessionStore.GetSessions(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get sessions: %v", err))
	}

	responses := make([]api.SessionsResponse, len(sessions))
	for i, sess := range sessions {
		responses[i] = api.SessionsResponse{
			ID:                         sess.ID,
			Title:                      sess.Title,
			CreatedAt:                  sess.CreatedAt.Format(time.RFC3339),
			NumMessages:                len(sess.GetAllMessages()),
			InputTokens:                sess.InputTokens,
			OutputTokens:               sess.OutputTokens,
			GetMostRecentAgentFilename: sess.GetMostRecentAgentFilename(),
			WorkingDir:                 sess.WorkingDir,
		}
	}
	return c.JSON(http.StatusOK, responses)
}

func (s *Server) getSessionsByAgent(c echo.Context) error {
	agentFilename := c.Param("id")
	if agentFilename == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id parameter is required")
	}

	sessions, err := s.sessionStore.GetSessionsByAgent(c.Request().Context(), agentFilename)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get sessions for agent: %v", err))
	}

	responses := make([]api.SessionsResponse, len(sessions))
	for i, sess := range sessions {
		responses[i] = api.SessionsResponse{
			ID:                         sess.ID,
			Title:                      sess.Title,
			CreatedAt:                  sess.CreatedAt.Format(time.RFC3339),
			NumMessages:                len(sess.GetAllMessages()),
			InputTokens:                sess.InputTokens,
			OutputTokens:               sess.OutputTokens,
			GetMostRecentAgentFilename: sess.GetMostRecentAgentFilename(),
			WorkingDir:                 sess.WorkingDir,
		}
	}
	return c.JSON(http.StatusOK, responses)
}

func (s *Server) createSession(c echo.Context) error {
	var sessionTemplate session.Session
	if err := c.Bind(&sessionTemplate); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	var opts []session.Opt
	opts = append(opts,
		session.WithMaxIterations(sessionTemplate.MaxIterations),
		session.WithToolsApproved(sessionTemplate.ToolsApproved),
	)

	if wd := strings.TrimSpace(sessionTemplate.WorkingDir); wd != "" {
		absWd, err := filepath.Abs(wd)
		if err != nil {
			slog.Error("Invalid working directory", "error", err)
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid working directory: %v", err))
		}
		info, err := os.Stat(absWd)
		if err != nil {
			slog.Error("Working directory not accessible", "error", err)
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("working directory not accessible: %v", err))
		}
		if !info.IsDir() {
			slog.Error("Working directory is not a directory")
			return echo.NewHTTPError(http.StatusBadRequest, "working directory must be a directory")
		}
		opts = append(opts, session.WithWorkingDir(absWd))
	}

	sess := session.New(opts...)

	if err := s.sessionStore.AddSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to persist session", "session_id", sess.ID, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create session: %v", err))
	}

	return c.JSON(http.StatusOK, sess)
}

func (s *Server) getSession(c echo.Context) error {
	sess, err := s.sessionStore.GetSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	params := api.PaginationParams{
		Limit:  api.DefaultLimit,
		Before: c.QueryParam("before"),
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}

	allMessages := sess.GetAllMessages()

	paginatedMessages, pagination, err := api.PaginateMessages(allMessages, params)
	if err != nil {
		slog.Error("Failed to paginate messages", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid pagination parameters: %v", err))
	}

	sr := api.SessionResponse{
		ID:            sess.ID,
		Title:         sess.Title,
		CreatedAt:     sess.CreatedAt,
		Messages:      paginatedMessages,
		ToolsApproved: sess.ToolsApproved,
		InputTokens:   sess.InputTokens,
		OutputTokens:  sess.OutputTokens,
		WorkingDir:    sess.WorkingDir,
		Pagination:    pagination,
	}

	return c.JSON(http.StatusOK, sr)
}

func (s *Server) resumeSession(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeSessionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	rt, exists := s.runtimes[sessionID]
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("runtime not found: %s", sessionID))
	}

	rt.Resume(c.Request().Context(), runtime.ResumeType(req.Confirmation))

	return c.JSON(http.StatusOK, map[string]string{"message": "session resumed"})
}

func (s *Server) deleteSession(c echo.Context) error {
	sessionID := c.Param("id")

	// Cancel the runtime context if it's still running
	s.cancelsMu.Lock()
	if cancel, exists := s.runtimeCancels[sessionID]; exists {
		slog.Debug("Cancelling runtime for session", "session_id", sessionID)
		cancel()
		delete(s.runtimeCancels, sessionID)
	}
	s.cancelsMu.Unlock()

	// Clean up the runtime
	if _, exists := s.runtimes[sessionID]; exists {
		slog.Debug("Removing runtime for session", "session_id", sessionID)
		delete(s.runtimes, sessionID)
	}

	// Delete the session from storage
	if err := s.sessionStore.DeleteSession(c.Request().Context(), sessionID); err != nil {
		slog.Error("Failed to delete session", "session_id", sessionID, "error", err)
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

	// Build a per-session team so Filesystem tool can be bound to session working dir
	sess, err := s.sessionStore.GetSession(c.Request().Context(), sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	// Copy runConfig and inject per-session working dir override
	rc := s.runConfig.Clone()
	rc.WorkingDir = sess.WorkingDir

	rt, err := s.runtimeForSession(c.Request().Context(), sess, agentFilename, currentAgent, rc)
	if err != nil {
		return err
	}

	// Receive messages from the API client
	var messages []api.Message
	if err := json.NewDecoder(c.Request().Body).Decode(&messages); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	for _, msg := range messages {
		sess.AddMessage(session.UserMessage(agentFilename, msg.Content, msg.MultiContent...))
	}

	if err := s.sessionStore.UpdateSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to update session in store", "session_id", sess.ID, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to update session: %v", err))
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	// Create a cancellable context for this stream
	streamCtx, cancel := context.WithCancel(c.Request().Context())
	s.cancelsMu.Lock()
	s.runtimeCancels[sess.ID] = cancel
	s.cancelsMu.Unlock()
	defer func() {
		s.cancelsMu.Lock()
		delete(s.runtimeCancels, sess.ID)
		s.cancelsMu.Unlock()
	}()

	streamChan := rt.RunStream(streamCtx, sess)
	for event := range streamChan {
		data, err := json.Marshal(event)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal event: %v", err))
		}
		fmt.Fprintf(c.Response(), "data: %s\n\n", string(data))
		c.Response().Flush()
	}

	if err := s.sessionStore.UpdateSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to final update session in store", "session_id", sess.ID, "error", err)
	}

	return nil
}

func (s *Server) runtimeForSession(ctx context.Context, sess *session.Session, agentFilename, currentAgent string, rc *config.RuntimeConfig) (runtime.Runtime, error) {
	rt, exists := s.runtimes[sess.ID]
	if exists {
		return rt, nil
	}

	// Load team - either reload from disk (local) or use in-memory team (OCI refs)
	t, err := s.loadTeamForSession(ctx, agentFilename, sess, rc)
	if err != nil {
		return nil, err
	}

	agent, err := t.Agent(currentAgent)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("agent not found: %v", err))
	}

	// Only set max iterations the first time the session is run
	// since on creation we can accept an empty sessionTemplate
	if len(sess.Messages) == 0 && sess.MaxIterations == 0 && agent.MaxIterations() > 0 {
		sess.MaxIterations = agent.MaxIterations()
	}

	opts := []runtime.Opt{
		runtime.WithCurrentAgent(currentAgent),
		runtime.WithManagedOAuth(false),
		runtime.WithRootSessionID(sess.ID),
	}
	rt, err = runtime.New(t, opts...)
	if err != nil {
		slog.Error("Failed to create runtime", "error", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create runtime: %v", err))
	}
	s.runtimes[sess.ID] = rt
	slog.Debug("Runtime created for session", "session_id", sess.ID)

	return rt, nil
}

// loadTeamForSession loads the appropriate team for a session, handling both
// local files (always reload) and OCI refs (reload only if session has custom workingDir).
func (s *Server) loadTeamForSession(ctx context.Context, agentFilename string, sess *session.Session, rc *config.RuntimeConfig) (*team.Team, error) {
	if s.hasAgentsDir() {
		// Has local agents path or directory: reload from disk to pick up changes
		loadPath := s.agentsPath
		if loadPath == "" {
			loadPath = filepath.Join(s.agentsDir, agentFilename)
		}

		t, err := teamloader.Load(ctx, loadPath, rc)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to load agent for session: %v", err))
		}
		return t, nil
	}

	// No local directory: use the already-loaded team from memory (OCI refs)
	t, exists := s.getTeam(agentFilename)
	if !exists {
		return nil, echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("agent not found: %s", agentFilename))
	}

	// If session has a custom working dir, reload the agent to pick it up
	if sess.WorkingDir != "" {
		if ociRef, ok := s.getOCIRef(agentFilename); ok {
			yamlContent, err := agentfile.FromStore(ociRef)
			if err != nil {
				slog.Error("Failed to load OCI agent from store", "agent", agentFilename, "oci_ref", ociRef, "error", err)
				return t, nil // Fall back to cached team
			}

			reloaded, err := teamloader.LoadFrom(
				ctx,
				teamloader.NewBytesSource([]byte(yamlContent)),
				rc,
				teamloader.WithID(agentFilename),
			)
			if err != nil {
				slog.Error("Failed to reload OCI agent with session working dir", "agent", agentFilename, "error", err)
				return t, nil // Fall back to cached team
			}
			return reloaded, nil
		}
	}

	return t, nil
}

func addYamlExt(filename string) string {
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		return filename
	}
	return filename + ".yaml"
}

func (s *Server) elicitation(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeElicitationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	rt, exists := s.runtimes[sessionID]
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("runtime not found: %s", sessionID)})
	}

	if err := rt.ResumeElicitation(c.Request().Context(), tools.ElicitationAction(req.Action), req.Content); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to resume elicitation: %v", err))
	}

	return c.JSON(http.StatusOK, nil)
}
