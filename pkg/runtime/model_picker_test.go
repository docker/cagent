package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
)

// staticToolSet is a simple ToolSet that returns a fixed list of tools.
type staticToolSet struct {
	tools []tools.Tool
}

func (s *staticToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return s.tools, nil
}

// TestModelChangeEmitsAgentInfo verifies that when a tool call changes the
// agent's model (like change_model does), an AgentInfoEvent with the new
// model ID is emitted even when the model stops in the same turn.
// This is the scenario where the TUI sidebar must be updated.
func TestModelChangeEmitsAgentInfo(t *testing.T) {
	newModel := &mockProvider{id: "openai/gpt-4o-mini"}

	// Stream 1: model calls the custom "switch_model" tool and stops.
	stream1 := newStreamBuilder().
		AddToolCallName("call_1", "switch_model").
		AddToolCallArguments("call_1", `{}`).
		AddStopWithUsage(5, 5).
		Build()

	// Stream 2: after the tool result is returned, model says "Done" and stops.
	stream2 := newStreamBuilder().
		AddContent("Model switched.").
		AddStopWithUsage(5, 5).
		Build()

	prov := &queueProvider{id: "test/original-model", streams: []chat.MessageStream{
		stream1,
		stream2,
	}}

	// Create a toolset that exposes the "switch_model" tool.
	switchToolSet := &staticToolSet{tools: []tools.Tool{
		{
			Name:        "switch_model",
			Description: "switch the model",
			Annotations: tools.ToolAnnotations{ReadOnlyHint: true},
		},
	}}

	root := agent.New("root", "test agent",
		agent.WithModel(prov),
		agent.WithToolSets(switchToolSet),
	)
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
	)
	require.NoError(t, err)

	// Register a custom handler that switches the agent's model override,
	// mimicking what handleChangeModel does internally.
	rt.toolMap["switch_model"] = func(_ context.Context, _ *session.Session, _ tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
		a2, _ := rt.team.Agent("root")
		a2.SetModelOverride(newModel)
		return tools.ResultSuccess("Model changed to openai/gpt-4o-mini"), nil
	}

	sess := session.New(session.WithUserMessage("Switch the model"), session.WithToolsApproved(true))
	sess.Title = "Test"

	evCh := rt.RunStream(t.Context(), sess)
	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}

	// Collect all AgentInfoEvents.
	var agentInfoEvents []*AgentInfoEvent
	for _, ev := range events {
		if ai, ok := ev.(*AgentInfoEvent); ok {
			agentInfoEvents = append(agentInfoEvents, ai)
		}
	}

	// There should be at least two AgentInfoEvents:
	// 1. The initial one with "test/original-model"
	// 2. One after the tool call with "openai/gpt-4o-mini"
	require.GreaterOrEqual(t, len(agentInfoEvents), 2, "expected at least 2 AgentInfoEvents, got %d", len(agentInfoEvents))

	// The first should show the original model.
	assert.Equal(t, "test/original-model", agentInfoEvents[0].Model)

	// At least one AgentInfoEvent should show the new model.
	foundNewModel := false
	for _, ai := range agentInfoEvents {
		if ai.Model == "openai/gpt-4o-mini" {
			foundNewModel = true
			break
		}
	}
	assert.True(t, foundNewModel, "expected an AgentInfoEvent with model 'openai/gpt-4o-mini'")
}
