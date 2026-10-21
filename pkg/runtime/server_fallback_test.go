package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

func (b *streamBuilder) AddServerFallback(from, to string) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index: 0,
			Delta: chat.MessageDelta{ServerFallback: &chat.ServerFallback{From: from, To: to}},
		}},
	})
	return b
}

func runServerFallbackStream(t *testing.T, stream *mockStream) (streamResult, []Event) {
	t.Helper()
	a := agent.New("root", "test", agent.WithModel(&mockProvider{id: "test/mock-model", stream: stream}))
	sess := session.New(session.WithUserMessage("go"))
	evCh := make(chan Event, 64)
	res, err := handleStream(t.Context(), nil, stream, a, nil, sess, NewChannelSink(evCh), defaultStreamIdleTimeout)
	require.NoError(t, err)
	close(evCh)
	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}
	return res, events
}

// A server fallback supersedes the declined model's tool calls and thinking:
// they must never run or be replayed. Its text stays, as the API keeps it.
func TestHandleStream_ServerFallbackDropsSupersededWork(t *testing.T) {
	t.Parallel()

	res, events := runServerFallbackStream(t, newStreamBuilder().
		AddReasoning("declined plan").
		AddContent("before ").
		AddToolCallName("declined", "rm_rf").
		AddToolCallArguments("declined", `{"path":"/"}`).
		AddServerFallback("claude-a", "claude-b").
		AddReasoning("new plan").
		AddContent("after").
		AddToolCallName("kept", "read").
		AddToolCallArguments("kept", `{}`).
		AddToolCallStopWithUsage(1, 1).
		Build())

	require.Len(t, res.Calls, 1)
	assert.Equal(t, "kept", res.Calls[0].ID)
	assert.Equal(t, "before after", res.Content)
	assert.Equal(t, "new plan", res.ReasoningContent)

	var warnings []string
	for _, ev := range events {
		if w, ok := ev.(*WarningEvent); ok {
			warnings = append(warnings, w.Message)
		}
	}
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "claude-a")
	assert.Contains(t, warnings[0], "claude-b")
}

func TestHandleStream_ServerFallbackWithoutSupersededWork(t *testing.T) {
	t.Parallel()

	res, _ := runServerFallbackStream(t, newStreamBuilder().
		AddServerFallback("claude-a", "claude-b").
		AddContent("hi").
		AddStopWithUsage(1, 1).
		Build())

	assert.Empty(t, res.Calls)
	assert.Equal(t, "hi", res.Content)
}
