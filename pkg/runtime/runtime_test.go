package runtime

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type mockStream struct {
	responses []chat.MessageStreamResponse
	idx       int
	closed    bool
}

func (m *mockStream) Recv() (chat.MessageStreamResponse, error) {
	if m.idx >= len(m.responses) {
		return chat.MessageStreamResponse{}, io.EOF
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func (m *mockStream) Close() { m.closed = true }

type streamBuilder struct{ responses []chat.MessageStreamResponse }

func newStreamBuilder() *streamBuilder {
	return &streamBuilder{responses: []chat.MessageStreamResponse{}}
}

func (b *streamBuilder) AddContent(content string) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index: 0,
			Delta: chat.MessageDelta{Content: content},
		}},
	})
	return b
}

func (b *streamBuilder) AddReasoning(content string) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index: 0,
			Delta: chat.MessageDelta{ReasoningContent: content},
		}},
	})
	return b
}

func (b *streamBuilder) AddToolCallName(id, name string) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index: 0,
			Delta: chat.MessageDelta{ToolCalls: []tools.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: tools.FunctionCall{Name: name},
			}}},
		}},
	})
	return b
}

func (b *streamBuilder) AddToolCallArguments(id, argsChunk string) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index: 0,
			Delta: chat.MessageDelta{ToolCalls: []tools.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: tools.FunctionCall{Arguments: argsChunk},
			}}},
		}},
	})
	return b
}

func (b *streamBuilder) AddStopWithUsage(input, output int) *streamBuilder {
	b.responses = append(b.responses, chat.MessageStreamResponse{
		Choices: []chat.MessageStreamChoice{{
			Index:        0,
			FinishReason: chat.FinishReasonStop,
		}},
		Usage: &chat.Usage{InputTokens: input, OutputTokens: output},
	})
	return b
}

func (b *streamBuilder) Build() *mockStream { return &mockStream{responses: b.responses} }

type mockProvider struct {
	id     string
	stream chat.MessageStream
}

func (m *mockProvider) ID() string { return m.id }

func (m *mockProvider) CreateChatCompletionStream(ctx context.Context, messages []chat.Message, _ []tools.Tool) (chat.MessageStream, error) {
	return m.stream, nil
}

func (m *mockProvider) CreateChatCompletion(ctx context.Context, messages []chat.Message) (string, error) {
	return "", nil
}

type mockProviderWithError struct {
	id string
}

func (m *mockProviderWithError) ID() string { return m.id }

func (m *mockProviderWithError) CreateChatCompletionStream(ctx context.Context, messages []chat.Message, _ []tools.Tool) (chat.MessageStream, error) {
	return nil, fmt.Errorf("simulated error creating chat completion stream")
}

func (m *mockProviderWithError) CreateChatCompletion(ctx context.Context, messages []chat.Message) (string, error) {
	return "", fmt.Errorf("simulated error creating chat completion")
}

type mockModelStore struct{}

func (m mockModelStore) GetModel(ctx context.Context, id string) (*modelsdev.Model, error) {
	return nil, nil
}

func runSession(t *testing.T, sess *session.Session, stream *mockStream) []Event {
	t.Helper()

	prov := &mockProvider{id: "test/mock-model", stream: stream}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess.Title = "Unit Test"

	evCh := rt.RunStream(t.Context(), sess)

	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}
	return events
}

func hasEventType(t *testing.T, events []Event, target Event) bool {
	t.Helper()

	want := reflect.TypeOf(target)
	for _, ev := range events {
		if reflect.TypeOf(ev) == want {
			return true
		}
	}
	return false
}

func TestSimple(t *testing.T) {
	stream := newStreamBuilder().
		AddContent("Hello").
		AddStopWithUsage(3, 2).
		Build()

	sess := session.New(session.WithUserMessage("", "Hi"))

	events := runSession(t, sess, stream)

	expectedEvents := []Event{
		UserMessage("Hi"),
		StreamStarted(sess.ID, "root"),
		AgentChoice("root", "Hello"),
		TokenUsage(3, 2, 5, 0, 0),
		StreamStopped(sess.ID, "root"),
	}

	require.Equal(t, expectedEvents, events)
}

func TestMultipleContentChunks(t *testing.T) {
	stream := newStreamBuilder().
		AddContent("Hello ").
		AddContent("there, ").
		AddContent("how ").
		AddContent("are ").
		AddContent("you?").
		AddStopWithUsage(8, 12).
		Build()

	sess := session.New(session.WithUserMessage("", "Please greet me"))

	events := runSession(t, sess, stream)

	expectedEvents := []Event{
		UserMessage("Please greet me"),
		StreamStarted(sess.ID, "root"),
		AgentChoice("root", "Hello "),
		AgentChoice("root", "there, "),
		AgentChoice("root", "how "),
		AgentChoice("root", "are "),
		AgentChoice("root", "you?"),
		TokenUsage(8, 12, 20, 0, 0),
		StreamStopped(sess.ID, "root"),
	}

	require.Equal(t, expectedEvents, events)
}

func TestWithReasoning(t *testing.T) {
	stream := newStreamBuilder().
		AddReasoning("Let me think about this...").
		AddReasoning(" I should respond politely.").
		AddContent("Hello, how can I help you?").
		AddStopWithUsage(10, 15).
		Build()

	sess := session.New(session.WithUserMessage("", "Hi"))

	events := runSession(t, sess, stream)

	expectedEvents := []Event{
		UserMessage("Hi"),
		StreamStarted(sess.ID, "root"),
		AgentChoiceReasoning("root", "Let me think about this..."),
		AgentChoiceReasoning("root", " I should respond politely."),
		AgentChoice("root", "Hello, how can I help you?"),
		TokenUsage(10, 15, 25, 0, 0),
		StreamStopped(sess.ID, "root"),
	}

	require.Equal(t, expectedEvents, events)
}

func TestMixedContentAndReasoning(t *testing.T) {
	stream := newStreamBuilder().
		AddReasoning("The user wants a greeting").
		AddContent("Hello!").
		AddReasoning(" I should be friendly").
		AddContent(" How can I help you today?").
		AddStopWithUsage(15, 20).
		Build()

	sess := session.New(session.WithUserMessage("", "Hi there"))

	events := runSession(t, sess, stream)

	expectedEvents := []Event{
		UserMessage("Hi there"),
		StreamStarted(sess.ID, "root"),
		AgentChoiceReasoning("root", "The user wants a greeting"),
		AgentChoice("root", "Hello!"),
		AgentChoiceReasoning("root", " I should be friendly"),
		AgentChoice("root", " How can I help you today?"),
		TokenUsage(15, 20, 35, 0, 0),
		StreamStopped(sess.ID, "root"),
	}

	require.Equal(t, expectedEvents, events)
}

func TestToolCallSequence(t *testing.T) {
	stream := newStreamBuilder().
		AddToolCallName("call_123", "test_tool").
		AddToolCallArguments("call_123", `{"param": "value"}`).
		AddStopWithUsage(5, 8).
		Build()

	sess := session.New(session.WithUserMessage("", "Please use the test tool"))

	events := runSession(t, sess, stream)

	require.True(t, hasEventType(t, events, &PartialToolCallEvent{}), "Expected PartialToolCallEvent")
	require.False(t, hasEventType(t, events, &ToolCallEvent{}), "Should not have ToolCallEvent without actual tool execution")

	require.True(t, hasEventType(t, events, &StreamStartedEvent{}), "Expected StreamStartedEvent")
	require.True(t, hasEventType(t, events, &StreamStoppedEvent{}), "Expected StreamStoppedEvent")
}

func TestErrorEvent(t *testing.T) {
	prov := &mockProviderWithError{id: "test/error-model"}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "Hi"))
	sess.Title = "Unit Test"

	evCh := rt.RunStream(t.Context(), sess)

	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}

	require.Len(t, events, 4)
	require.IsType(t, &UserMessageEvent{}, events[0])
	require.IsType(t, &StreamStartedEvent{}, events[1])
	require.IsType(t, &ErrorEvent{}, events[2])
	require.IsType(t, &StreamStoppedEvent{}, events[3])

	// Check the error message contains our test error
	errorEvent := events[2].(*ErrorEvent)
	require.Contains(t, errorEvent.Error, "simulated error")
}

func TestContextCancellation(t *testing.T) {
	stream := newStreamBuilder().
		AddContent("This should not complete").
		AddStopWithUsage(10, 5).
		Build()

	prov := &mockProvider{id: "test/mock-model", stream: stream}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "Hi"))
	sess.Title = "Unit Test"

	ctx, cancel := context.WithCancel(context.Background())
	evCh := rt.RunStream(ctx, sess)

	cancel()

	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}

	require.GreaterOrEqual(t, len(events), 2)
	require.IsType(t, &UserMessageEvent{}, events[0])
	require.IsType(t, &StreamStartedEvent{}, events[1])
	require.IsType(t, &StreamStoppedEvent{}, events[len(events)-1])
}

func TestToolCallVariations(t *testing.T) {
	tests := []struct {
		name          string
		streamBuilder func() *streamBuilder
		description   string
	}{
		{
			name: "tool_call_with_empty_args",
			streamBuilder: func() *streamBuilder {
				return newStreamBuilder().
					AddToolCallName("call_1", "empty_tool").
					AddToolCallArguments("call_1", "{}").
					AddStopWithUsage(3, 5)
			},
			description: "Tool call with empty JSON arguments",
		},
		{
			name: "multiple_tool_calls",
			streamBuilder: func() *streamBuilder {
				return newStreamBuilder().
					AddToolCallName("call_1", "tool_one").
					AddToolCallArguments("call_1", `{"param":"value1"}`).
					AddToolCallName("call_2", "tool_two").
					AddToolCallArguments("call_2", `{"param":"value2"}`).
					AddStopWithUsage(8, 12)
			},
			description: "Multiple tool calls in sequence",
		},
		{
			name: "tool_call_with_fragmented_args",
			streamBuilder: func() *streamBuilder {
				return newStreamBuilder().
					AddToolCallName("call_1", "fragmented_tool").
					AddToolCallArguments("call_1", `{"long`).
					AddToolCallArguments("call_1", `_param": "`).
					AddToolCallArguments("call_1", `some_value"}`).
					AddStopWithUsage(5, 8)
			},
			description: "Tool call with arguments streamed in fragments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := tt.streamBuilder().Build()
			sess := session.New(session.WithUserMessage("", "Use tools"))
			events := runSession(t, sess, stream)

			require.True(t, hasEventType(t, events, &PartialToolCallEvent{}), "Expected PartialToolCallEvent for %s", tt.description)
			require.True(t, hasEventType(t, events, &StreamStartedEvent{}), "Expected StreamStartedEvent")
			require.True(t, hasEventType(t, events, &StreamStoppedEvent{}), "Expected StreamStoppedEvent")
		})
	}
}

func TestSessionWithoutUserMessage(t *testing.T) {
	stream := newStreamBuilder().AddContent("OK").AddStopWithUsage(1, 1).Build()

	sess := session.New()
	sess.SendUserMessage = false

	events := runSession(t, sess, stream)

	require.True(t, hasEventType(t, events, &StreamStartedEvent{}), "Expected StreamStartedEvent")
	require.True(t, hasEventType(t, events, &StreamStoppedEvent{}), "Expected StreamStoppedEvent")
	require.False(t, hasEventType(t, events, &UserMessageEvent{}), "Should not have UserMessageEvent when SendUserMessage is false")
}
