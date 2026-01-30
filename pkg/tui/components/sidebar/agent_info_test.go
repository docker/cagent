package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/service"
)

func TestSidebar_SetAgentInfoUpdatesAvailableAgents(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)

	// Set initial team info with original model
	m.SetTeamInfo([]runtime.AgentDetails{
		{
			Name:        "root",
			Description: "Test agent",
			Provider:    "openai",
			Model:       "gpt-4o-mini",
		},
	})

	// Verify initial state
	require.Len(t, m.availableAgents, 1)
	assert.Equal(t, "openai", m.availableAgents[0].Provider)
	assert.Equal(t, "gpt-4o-mini", m.availableAgents[0].Model)

	// Now simulate a model switch via SetAgentInfo with new model
	m.SetAgentInfo("root", "anthropic/claude-sonnet-4-0", "Test agent")

	// Verify the model was updated in availableAgents
	require.Len(t, m.availableAgents, 1)
	assert.Equal(t, "anthropic", m.availableAgents[0].Provider, "Provider should be updated")
	assert.Equal(t, "claude-sonnet-4-0", m.availableAgents[0].Model, "Model should be updated")
}

func TestSidebar_SetAgentInfoUpdatesCorrectAgent(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)

	// Set up multiple agents
	m.SetTeamInfo([]runtime.AgentDetails{
		{
			Name:        "root",
			Description: "Root agent",
			Provider:    "openai",
			Model:       "gpt-4o",
		},
		{
			Name:        "helper",
			Description: "Helper agent",
			Provider:    "anthropic",
			Model:       "claude-sonnet-4-0",
		},
	})

	// Switch the model for the helper agent
	m.SetAgentInfo("helper", "google/gemini-2.0-flash", "Helper agent")

	// Verify only the helper agent's model was updated
	require.Len(t, m.availableAgents, 2)
	assert.Equal(t, "openai", m.availableAgents[0].Provider, "Root provider should not change")
	assert.Equal(t, "gpt-4o", m.availableAgents[0].Model, "Root model should not change")
	assert.Equal(t, "google", m.availableAgents[1].Provider, "Helper provider should be updated")
	assert.Equal(t, "gemini-2.0-flash", m.availableAgents[1].Model, "Helper model should be updated")
}

func TestSidebar_SetAgentInfoWithModelIDWithoutProvider(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)

	// Set initial team info
	m.SetTeamInfo([]runtime.AgentDetails{
		{
			Name:        "root",
			Description: "Test agent",
			Provider:    "openai",
			Model:       "gpt-4o-mini",
		},
	})

	// Switch to a model ID without provider prefix (shouldn't happen but handle gracefully)
	m.SetAgentInfo("root", "some-model", "Test agent")

	// Verify the model was set (provider should remain unchanged)
	require.Len(t, m.availableAgents, 1)
	assert.Equal(t, "openai", m.availableAgents[0].Provider, "Provider should not change for non-prefixed model")
	assert.Equal(t, "some-model", m.availableAgents[0].Model, "Model should be updated to the full ID")
}

func TestSidebar_SetAgentInfoForNonExistentAgent(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)

	// Set initial team info
	m.SetTeamInfo([]runtime.AgentDetails{
		{
			Name:        "root",
			Description: "Test agent",
			Provider:    "openai",
			Model:       "gpt-4o-mini",
		},
	})

	// Try to set info for a non-existent agent (should not panic or modify existing agents)
	m.SetAgentInfo("nonexistent", "anthropic/claude-sonnet-4-0", "Some agent")

	// Verify the existing agent was not modified
	require.Len(t, m.availableAgents, 1)
	assert.Equal(t, "openai", m.availableAgents[0].Provider)
	assert.Equal(t, "gpt-4o-mini", m.availableAgents[0].Model)
}

func TestSidebar_SetAgentInfoWithEmptyModelID(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)

	// Set initial team info
	m.SetTeamInfo([]runtime.AgentDetails{
		{
			Name:        "root",
			Description: "Test agent",
			Provider:    "openai",
			Model:       "gpt-4o-mini",
		},
	})

	// Call SetAgentInfo with empty modelID (should not modify availableAgents)
	m.SetAgentInfo("root", "", "Test agent")

	// Verify the existing agent's model was not modified
	require.Len(t, m.availableAgents, 1)
	assert.Equal(t, "openai", m.availableAgents[0].Provider)
	assert.Equal(t, "gpt-4o-mini", m.availableAgents[0].Model)
}
