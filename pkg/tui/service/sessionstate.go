package service

import (
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/types"
)

// SessionState holds shared state across the TUI application.
// This provides a centralized location for state that needs to be
// accessible by multiple components.
type SessionState struct {
	// SplitDiffView determines whether diff views should be shown side-by-side (true)
	// or unified (false)
	SplitDiffView   bool
	YoloMode        bool
	Thinking        bool // true = enabled (default), false = disabled
	HideToolResults bool
	PreviousMessage *types.Message
	// CurrentAgent is the name of the currently active agent for user messages
	CurrentAgent string
}

// NewSessionState creates a new SessionState with default values.
func NewSessionState(sessionState *session.Session) *SessionState {
	return &SessionState{
		SplitDiffView:   true, // Default to split view
		YoloMode:        sessionState.ToolsApproved,
		Thinking:        sessionState.Thinking,
		HideToolResults: sessionState.HideToolResults,
	}
}

// ToggleSplitDiffView toggles between split and unified diff view modes.
func (s *SessionState) ToggleSplitDiffView() {
	s.SplitDiffView = !s.SplitDiffView
}

func (s *SessionState) SetYoloMode(enabled bool) {
	s.YoloMode = enabled
}

func (s *SessionState) SetHideToolResults(enabled bool) {
	s.HideToolResults = enabled
}

func (s *SessionState) ToggleHideToolResults() {
	s.HideToolResults = !s.HideToolResults
}

func (s *SessionState) SetCurrentAgent(agentName string) {
	s.CurrentAgent = agentName
}

func (s *SessionState) SetThinking(enabled bool) {
	s.Thinking = enabled
}
