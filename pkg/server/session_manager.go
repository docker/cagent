package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
)

type activeRuntimes struct {
	runtime runtime.Runtime
	cancel  context.CancelFunc
}

// SessionManager manages sessions for HTTP and Connect-RPC servers.
type SessionManager struct {
	runtimeSessions *concurrent.Map[string, *activeRuntimes]
	sessionStore    session.Store
	Sources         config.Sources

	// TODO: We have to do something about this, it's weird, session creation should send everything that is needed.
	// This is only used for the working directory...
	runConfig *config.RuntimeConfig

	refreshInterval time.Duration

	mux sync.Mutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager(ctx context.Context, sources config.Sources, sessionStore session.Store, refreshInterval time.Duration, runConfig *config.RuntimeConfig) *SessionManager {
	loaders := make(config.Sources)
	for name, source := range sources {
		loaders[name] = newSourceLoader(ctx, source, refreshInterval)
	}

	sm := &SessionManager{
		runtimeSessions: concurrent.NewMap[string, *activeRuntimes](),
		sessionStore:    sessionStore,
		Sources:         loaders,
		refreshInterval: refreshInterval,
		runConfig:       runConfig,
	}

	return sm
}

// GetSession retrieves a session by ID.
func (sm *SessionManager) GetSession(ctx context.Context, id string) (*session.Session, error) {
	sess, err := sm.sessionStore.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// CreateSession creates a new session from a template.
func (sm *SessionManager) CreateSession(ctx context.Context, sessionTemplate *session.Session) (*session.Session, error) {
	var opts []session.Opt
	opts = append(opts,
		session.WithMaxIterations(sessionTemplate.MaxIterations),
		session.WithToolsApproved(sessionTemplate.ToolsApproved),
	)

	if wd := strings.TrimSpace(sessionTemplate.WorkingDir); wd != "" {
		absWd, err := filepath.Abs(wd)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(absWd)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("working directory must be a directory")
		}
		opts = append(opts, session.WithWorkingDir(absWd))
	}

	if sessionTemplate.Permissions != nil {
		opts = append(opts, session.WithPermissions(sessionTemplate.Permissions))
	}

	sess := session.New(opts...)
	return sess, sm.sessionStore.AddSession(ctx, sess)
}

// GetSessions retrieves all sessions.
func (sm *SessionManager) GetSessions(ctx context.Context) ([]*session.Session, error) {
	sessions, err := sm.sessionStore.GetSessions(ctx)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// DeleteSession deletes a session by ID.
func (sm *SessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	sess, err := sm.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	if err := sm.sessionStore.DeleteSession(ctx, sessionID); err != nil {
		return err
	}

	if sessionRuntime, ok := sm.runtimeSessions.Load(sess.ID); ok {
		sessionRuntime.cancel()
		sm.runtimeSessions.Delete(sess.ID)
	}

	return nil
}

// RunSession runs a session with the given messages.
func (sm *SessionManager) RunSession(ctx context.Context, sessionID, agentFilename, currentAgent string, messages []api.Message) (<-chan runtime.Event, error) {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	sess, err := sm.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	rc := sm.runConfig.Clone()
	rc.WorkingDir = sess.WorkingDir
	for _, msg := range messages {
		sess.AddMessage(session.UserMessage(msg.Content, msg.MultiContent...))
	}

	if err := sm.sessionStore.UpdateSession(ctx, sess); err != nil {
		return nil, err
	}

	runtimeSession, exists := sm.runtimeSessions.Load(sessionID)
	streamCtx, cancel := context.WithCancel(ctx)
	if !exists {
		rt, err := sm.runtimeForSession(ctx, sess, agentFilename, currentAgent, rc)
		if err != nil {
			cancel()
			return nil, err
		}
		runtimeSession = &activeRuntimes{
			runtime: rt,
			cancel:  cancel,
		}
		sm.runtimeSessions.Store(sessionID, runtimeSession)
	}

	streamChan := make(chan runtime.Event)

	go func() {
		stream := runtimeSession.runtime.RunStream(streamCtx, sess)
		defer cancel()
		defer close(streamChan)
		for event := range stream {
			if streamCtx.Err() != nil {
				return
			}
			streamChan <- event
		}

		if err := sm.sessionStore.UpdateSession(ctx, sess); err != nil {
			return
		}
	}()

	return streamChan, nil
}

// ResumeSession resumes a paused session.
func (sm *SessionManager) ResumeSession(ctx context.Context, sessionID, confirmation string) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	rt, exists := sm.runtimeSessions.Load(sessionID)
	if !exists {
		return errors.New("session not found")
	}

	rt.runtime.Resume(ctx, runtime.ResumeType(confirmation))
	return nil
}

// ResumeElicitation resumes an elicitation request.
func (sm *SessionManager) ResumeElicitation(ctx context.Context, sessionID, action string, content map[string]any) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	rt, exists := sm.runtimeSessions.Load(sessionID)
	if !exists {
		return errors.New("session not found")
	}

	return rt.runtime.ResumeElicitation(ctx, tools.ElicitationAction(action), content)
}

// ToggleToolApproval toggles the tool approval mode for a session.
func (sm *SessionManager) ToggleToolApproval(ctx context.Context, sessionID string) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	sess, err := sm.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.ToolsApproved = !sess.ToolsApproved

	return sm.sessionStore.UpdateSession(ctx, sess)
}

// ToggleThinking toggles the thinking mode for a session.
func (sm *SessionManager) ToggleThinking(ctx context.Context, sessionID string) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	sess, err := sm.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.Thinking = !sess.Thinking

	return sm.sessionStore.UpdateSession(ctx, sess)
}

// UpdateSessionPermissions updates the permissions for a session.
func (sm *SessionManager) UpdateSessionPermissions(ctx context.Context, sessionID string, perms *session.PermissionsConfig) error {
	sm.mux.Lock()
	defer sm.mux.Unlock()
	sess, err := sm.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.Permissions = perms

	return sm.sessionStore.UpdateSession(ctx, sess)
}

func (sm *SessionManager) runtimeForSession(ctx context.Context, sess *session.Session, agentFilename, currentAgent string, rc *config.RuntimeConfig) (runtime.Runtime, error) {
	rt, exists := sm.runtimeSessions.Load(sess.ID)
	if exists && rt.runtime != nil {
		return rt.runtime, nil
	}

	t, err := sm.loadTeam(ctx, agentFilename, rc)
	if err != nil {
		return nil, err
	}

	agent, err := t.Agent(currentAgent)
	if err != nil {
		return nil, err
	}
	sess.MaxIterations = agent.MaxIterations()

	opts := []runtime.Opt{
		runtime.WithCurrentAgent(currentAgent),
		runtime.WithManagedOAuth(false),
		runtime.WithSessionStore(sm.sessionStore),
	}
	run, err := runtime.New(t, opts...)
	if err != nil {
		return nil, err
	}

	sm.runtimeSessions.Store(sess.ID, &activeRuntimes{
		runtime: run,
	})

	slog.Debug("Runtime created for session", "session_id", sess.ID)

	return run, nil
}

func (sm *SessionManager) loadTeam(ctx context.Context, agentFilename string, runConfig *config.RuntimeConfig) (*team.Team, error) {
	agentSource, found := sm.Sources[agentFilename]
	if !found {
		return nil, fmt.Errorf("agent not found: %s", agentFilename)
	}

	return teamloader.Load(ctx, agentSource, runConfig)
}
