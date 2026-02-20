package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/service"
)

func TestContextPercent_SingleSession(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	m.SetTokenUsage(&runtime.TokenUsageEvent{
		SessionID:    "session-1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Usage: &runtime.Usage{
			InputTokens:   5000,
			OutputTokens:  5000,
			ContextLength: 10000,
			ContextLimit:  100000,
		},
	})

	assert.Equal(t, "10%", m.contextPercent())
}

func TestContextPercent_MultipleSessionsWithCurrentAgent(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	// Root agent session
	m.SetTokenUsage(&runtime.TokenUsageEvent{
		SessionID:    "session-root",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Usage: &runtime.Usage{
			InputTokens:   20000,
			OutputTokens:  10000,
			ContextLength: 30000,
			ContextLimit:  100000,
		},
	})

	// Child agent session (from transfer_task)
	m.SetTokenUsage(&runtime.TokenUsageEvent{
		SessionID:    "session-child",
		AgentContext: runtime.AgentContext{AgentName: "developer"},
		Usage: &runtime.Usage{
			InputTokens:   8000,
			OutputTokens:  2000,
			ContextLength: 10000,
			ContextLimit:  200000,
		},
	})

	// Current agent is the child — should show child's percentage
	m.currentAgent = "developer"
	assert.Equal(t, "5%", m.contextPercent())

	// Switch back to root agent — should show root's percentage
	m.currentAgent = "root"
	assert.Equal(t, "30%", m.contextPercent())
}

func TestContextPercent_NoContextLimit(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	m.SetTokenUsage(&runtime.TokenUsageEvent{
		SessionID:    "session-1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Usage: &runtime.Usage{
			InputTokens:   5000,
			OutputTokens:  5000,
			ContextLength: 10000,
			ContextLimit:  0, // No context limit
		},
	})

	m.currentAgent = "root"
	assert.Empty(t, m.contextPercent())
}

func TestContextPercent_EmptyUsage(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	assert.Empty(t, m.contextPercent())
}

func TestContextPercent_FallbackToSingleSession(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	// Session with no agent mapping (e.g., restored from persistence)
	m.sessionUsage["session-1"] = &runtime.Usage{
		InputTokens:   5000,
		OutputTokens:  5000,
		ContextLength: 10000,
		ContextLimit:  100000,
	}
	// No sessionAgent entry, no currentAgent set

	assert.Equal(t, "10%", m.contextPercent())
}
