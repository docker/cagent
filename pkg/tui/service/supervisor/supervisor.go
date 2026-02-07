// Package supervisor manages multiple background agent sessions.
package supervisor

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/messages"
)

// SessionRunner represents a running background session.
type SessionRunner struct {
	ID         string
	App        *app.App
	WorkingDir string
	Title      string
	IsRunning  bool // True when stream is active
	NeedsAttn  bool // True when user attention is needed
	cancel     context.CancelFunc
	cleanup    func()
}

// SessionSpawner is a function that creates new sessions.
// It takes a working directory and returns the app, session, and cleanup function.
type SessionSpawner func(ctx context.Context, workingDir string) (*app.App, *session.Session, func(), error)

// Supervisor manages multiple concurrent agent sessions.
type Supervisor struct {
	mu       sync.RWMutex
	runners  map[string]*SessionRunner
	order    []string // Maintains tab order
	activeID string
	spawner  SessionSpawner
	program  *tea.Program

	// programReady is closed when SetProgram is called. Subscription goroutines
	// wait on this before consuming events so that startup events (welcome message,
	// agent info, tool info) are not silently dropped.
	programReady     chan struct{}
	programReadyOnce sync.Once
}

// New creates a new supervisor.
func New(spawner SessionSpawner) *Supervisor {
	return &Supervisor{
		runners:      make(map[string]*SessionRunner),
		spawner:      spawner,
		programReady: make(chan struct{}),
	}
}

// SetProgram sets the Bubble Tea program for sending messages.
func (s *Supervisor) SetProgram(p *tea.Program) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.program = p
	s.programReadyOnce.Do(func() {
		close(s.programReady)
	})
}

// AddSession adds an existing session to the supervisor.
func (s *Supervisor) AddSession(ctx context.Context, a *app.App, sess *session.Session, workingDir string, cleanup func()) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner := &SessionRunner{
		ID:         sess.ID,
		App:        a,
		WorkingDir: workingDir,
		Title:      sess.Title,
		cleanup:    cleanup,
	}

	// Create a cancellable context for this session
	sessionCtx, cancel := context.WithCancel(ctx)
	runner.cancel = cancel

	s.runners[sess.ID] = runner
	s.order = append(s.order, sess.ID)

	if s.activeID == "" {
		s.activeID = sess.ID
	}

	// Start the subscription goroutine with routing
	go s.subscribeWithRouting(sessionCtx, a, sess.ID)

	return sess.ID
}

// SpawnSession creates and adds a new session.
func (s *Supervisor) SpawnSession(ctx context.Context, workingDir string) (string, error) {
	a, sess, cleanup, err := s.spawner(ctx, workingDir)
	if err != nil {
		return "", err
	}

	sessionID := s.AddSession(ctx, a, sess, workingDir, cleanup)
	return sessionID, nil
}

// subscribeWithRouting subscribes to app events and wraps them with session ID.
// It waits for the program to be set before consuming events so that startup
// events (welcome message, agent/team/tool info) are not dropped.
func (s *Supervisor) subscribeWithRouting(ctx context.Context, a *app.App, sessionID string) {
	// Wait for the program to be available before consuming any events.
	// Events are buffered in app.events, so nothing is lost during this wait.
	select {
	case <-s.programReady:
	case <-ctx.Done():
		return
	}

	send := func(msg tea.Msg) {
		s.mu.RLock()
		p := s.program
		s.mu.RUnlock()

		if p == nil {
			return
		}

		// Check if this is a runtime event that should update state
		s.handleRuntimeEvent(sessionID, msg)

		// Wrap the message with session ID
		p.Send(messages.RoutedMsg{
			SessionID: sessionID,
			Inner:     msg,
		})
	}

	a.SubscribeWith(ctx, send)
}

// handleRuntimeEvent updates runner state based on runtime events.
func (s *Supervisor) handleRuntimeEvent(sessionID string, msg tea.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner, ok := s.runners[sessionID]
	if !ok {
		return
	}

	switch ev := msg.(type) {
	case *runtime.StreamStartedEvent:
		runner.IsRunning = true
		s.notifyTabsUpdated()

	case *runtime.StreamStoppedEvent:
		runner.IsRunning = false
		s.notifyTabsUpdated()

	case *runtime.SessionTitleEvent:
		runner.Title = ev.Title
		s.notifyTabsUpdated()

	case *runtime.ToolCallConfirmationEvent, *runtime.MaxIterationsReachedEvent:
		// These require user attention
		if sessionID != s.activeID {
			runner.NeedsAttn = true
			s.notifyTabsUpdated()
		}
	}
}

// notifyTabsUpdated sends a tabs updated message (must be called with lock held).
func (s *Supervisor) notifyTabsUpdated() {
	p := s.program
	if p == nil {
		return
	}

	tabs := s.buildTabInfoLocked()
	activeIdx := s.activeIndexLocked()

	// Send asynchronously to avoid blocking.
	// Capture p locally so the goroutine doesn't race on s.program.
	go p.Send(messages.TabsUpdatedMsg{
		Tabs:      tabs,
		ActiveIdx: activeIdx,
	})
}

// buildTabInfoLocked builds tab info (must be called with lock held).
func (s *Supervisor) buildTabInfoLocked() []messages.TabInfo {
	tabs := make([]messages.TabInfo, 0, len(s.order))
	for _, id := range s.order {
		runner := s.runners[id]
		if runner == nil {
			continue
		}

		title := runner.Title
		if title == "" {
			title = filepath.Base(runner.WorkingDir)
		}

		tabs = append(tabs, messages.TabInfo{
			SessionID:      id,
			Title:          title,
			IsActive:       id == s.activeID,
			IsRunning:      runner.IsRunning,
			NeedsAttention: runner.NeedsAttn,
		})
	}
	return tabs
}

// activeIndexLocked returns the index of the active tab (must be called with lock held).
func (s *Supervisor) activeIndexLocked() int {
	for i, id := range s.order {
		if id == s.activeID {
			return i
		}
	}
	return 0
}

// SwitchTo switches to a different session.
func (s *Supervisor) SwitchTo(sessionID string) *SessionRunner {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner, ok := s.runners[sessionID]
	if !ok {
		return nil
	}

	s.activeID = sessionID
	runner.NeedsAttn = false // Clear attention flag when switching to this tab
	s.notifyTabsUpdated()

	return runner
}

// ActiveRunner returns the currently active session runner.
func (s *Supervisor) ActiveRunner() *SessionRunner {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.runners[s.activeID]
}

// GetRunner returns the runner for the given session ID, or nil if not found.
func (s *Supervisor) GetRunner(sessionID string) *SessionRunner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runners[sessionID]
}

// SetRunnerTitle updates the title of the runner for the given session ID.
// It also triggers a tab update notification.
func (s *Supervisor) SetRunnerTitle(sessionID, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runner, ok := s.runners[sessionID]; ok {
		runner.Title = title
		s.notifyTabsUpdated()
	}
}

// ActiveID returns the ID of the currently active session.
func (s *Supervisor) ActiveID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeID
}

// CloseSession closes a session and removes it from the supervisor.
func (s *Supervisor) CloseSession(sessionID string) (nextActiveID string) {
	s.mu.Lock()

	runner, ok := s.runners[sessionID]
	if !ok {
		nextActiveID = s.activeID
		s.mu.Unlock()
		return nextActiveID
	}

	// Cancel the session context
	if runner.cancel != nil {
		runner.cancel()
	}
	cleanup := runner.cleanup

	// Remove from maps
	delete(s.runners, sessionID)

	// Remove from order slice
	for i, id := range s.order {
		if id == sessionID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}

	// If this was the active session, switch to another
	if s.activeID == sessionID {
		if len(s.order) > 0 {
			s.activeID = s.order[0]
		} else {
			s.activeID = ""
		}
	}

	s.notifyTabsUpdated()
	nextActiveID = s.activeID
	s.mu.Unlock()

	// Run cleanup outside the lock so it can't deadlock.
	if cleanup != nil {
		go cleanup()
	}

	return nextActiveID
}

// Count returns the number of sessions.
func (s *Supervisor) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.runners)
}

// GetTabs returns the current tab info.
func (s *Supervisor) GetTabs() ([]messages.TabInfo, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildTabInfoLocked(), s.activeIndexLocked()
}

// Shutdown closes all sessions.
func (s *Supervisor) Shutdown() {
	s.mu.Lock()

	// Cancel all contexts first, then collect cleanup functions.
	var cleanups []func()
	for _, runner := range s.runners {
		if runner.cancel != nil {
			runner.cancel()
		}
		if runner.cleanup != nil {
			cleanups = append(cleanups, runner.cleanup)
		}
	}

	count := len(s.runners)
	s.runners = make(map[string]*SessionRunner)
	s.order = nil
	s.activeID = ""
	s.mu.Unlock()

	// Run cleanups outside the lock so they can't deadlock.
	for _, cleanup := range cleanups {
		cleanup()
	}

	slog.Debug("Supervisor shutdown complete", "sessions", count)
}
