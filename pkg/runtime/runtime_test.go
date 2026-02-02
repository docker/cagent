package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/permissions"
	"github.com/docker/cagent/pkg/rag"
	"github.com/docker/cagent/pkg/rag/database"
	"github.com/docker/cagent/pkg/rag/strategy"
	ragtypes "github.com/docker/cagent/pkg/rag/types"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type stubToolSet struct {
	startErr error
	tools    []tools.Tool
	listErr  error
}

// Verify interface compliance
var (
	_ tools.ToolSet   = (*stubToolSet)(nil)
	_ tools.Startable = (*stubToolSet)(nil)
)

func newStubToolSet(startErr error, toolsList []tools.Tool, listErr error) tools.ToolSet {
	return &stubToolSet{
		startErr: startErr,
		tools:    toolsList,
		listErr:  listErr,
	}
}

func (s *stubToolSet) Start(context.Context) error { return s.startErr }
func (s *stubToolSet) Stop(context.Context) error  { return nil }
func (s *stubToolSet) Tools(context.Context) ([]tools.Tool, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.tools, nil
}

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

func (b *streamBuilder) AddStopWithUsage(input, output int64) *streamBuilder {
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

func (m *mockProvider) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return m.stream, nil
}

func (m *mockProvider) BaseConfig() base.Config { return base.Config{} }

func (m *mockProvider) MaxTokens() int { return 0 }

type mockProviderWithError struct {
	id string
}

func (m *mockProviderWithError) ID() string { return m.id }

func (m *mockProviderWithError) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return nil, fmt.Errorf("simulated error creating chat completion stream")
}

func (m *mockProviderWithError) BaseConfig() base.Config { return base.Config{} }

func (m *mockProviderWithError) MaxTokens() int { return 0 }

type mockModelStore struct{}

func (m mockModelStore) GetModel(context.Context, string) (*modelsdev.Model, error) {
	return nil, nil
}

func runSession(t *testing.T, sess *session.Session, stream *mockStream) []Event {
	t.Helper()

	prov := &mockProvider{id: "test/mock-model", stream: stream}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
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

	sess := session.New(session.WithUserMessage("Hi"))

	events := runSession(t, sess, stream)

	// Extract the actual message from MessageAddedEvent to use in comparison
	// (it contains dynamic fields like CreatedAt that we can't predict)
	require.Len(t, events, 9)
	msgAdded := events[6].(*MessageAddedEvent)
	require.NotNil(t, msgAdded.Message)
	require.Equal(t, "Hello", msgAdded.Message.Message.Content)
	require.Equal(t, chat.MessageRoleAssistant, msgAdded.Message.Message.Role)

	expectedEvents := []Event{
		AgentInfo("root", "test/mock-model", "", ""),
		TeamInfo([]AgentDetails{{Name: "root", Provider: "test", Model: "mock-model"}}, "root"),
		ToolsetInfo(0, false, "root"),
		UserMessage("Hi", sess.ID),
		StreamStarted(sess.ID, "root"),
		AgentChoice("root", "Hello"),
		MessageAdded(sess.ID, msgAdded.Message, "root"),
		TokenUsageWithMessage(sess.ID, "root", 3, 2, 5, 0, 0, &MessageUsage{
			Usage: chat.Usage{InputTokens: 3, OutputTokens: 2},
			Model: "test/mock-model",
		}),
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

	sess := session.New(session.WithUserMessage("Please greet me"))

	events := runSession(t, sess, stream)

	// Extract the actual message from MessageAddedEvent to use in comparison
	// (it contains dynamic fields like CreatedAt that we can't predict)
	require.Len(t, events, 13)
	msgAdded := events[10].(*MessageAddedEvent)
	require.NotNil(t, msgAdded.Message)

	expectedEvents := []Event{
		AgentInfo("root", "test/mock-model", "", ""),
		TeamInfo([]AgentDetails{{Name: "root", Provider: "test", Model: "mock-model"}}, "root"),
		ToolsetInfo(0, false, "root"),
		UserMessage("Please greet me", sess.ID),
		StreamStarted(sess.ID, "root"),
		AgentChoice("root", "Hello "),
		AgentChoice("root", "there, "),
		AgentChoice("root", "how "),
		AgentChoice("root", "are "),
		AgentChoice("root", "you?"),
		MessageAdded(sess.ID, msgAdded.Message, "root"),
		TokenUsageWithMessage(sess.ID, "root", 8, 12, 20, 0, 0, &MessageUsage{
			Usage: chat.Usage{InputTokens: 8, OutputTokens: 12},
			Model: "test/mock-model",
		}),
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

	sess := session.New(session.WithUserMessage("Hi"))

	events := runSession(t, sess, stream)

	// Extract the actual message from MessageAddedEvent to use in comparison
	// (it contains dynamic fields like CreatedAt that we can't predict)
	require.Len(t, events, 11)
	msgAdded := events[8].(*MessageAddedEvent)
	require.NotNil(t, msgAdded.Message)

	expectedEvents := []Event{
		AgentInfo("root", "test/mock-model", "", ""),
		TeamInfo([]AgentDetails{{Name: "root", Provider: "test", Model: "mock-model"}}, "root"),
		ToolsetInfo(0, false, "root"),
		UserMessage("Hi", sess.ID),
		StreamStarted(sess.ID, "root"),
		AgentChoiceReasoning("root", "Let me think about this..."),
		AgentChoiceReasoning("root", " I should respond politely."),
		AgentChoice("root", "Hello, how can I help you?"),
		MessageAdded(sess.ID, msgAdded.Message, "root"),
		TokenUsageWithMessage(sess.ID, "root", 10, 15, 25, 0, 0, &MessageUsage{
			Usage: chat.Usage{InputTokens: 10, OutputTokens: 15},
			Model: "test/mock-model",
		}),
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

	sess := session.New(session.WithUserMessage("Hi there"))

	events := runSession(t, sess, stream)

	// Extract the actual message from MessageAddedEvent to use in comparison
	// (it contains dynamic fields like CreatedAt that we can't predict)
	require.Len(t, events, 12)
	msgAdded := events[9].(*MessageAddedEvent)
	require.NotNil(t, msgAdded.Message)

	expectedEvents := []Event{
		AgentInfo("root", "test/mock-model", "", ""),
		TeamInfo([]AgentDetails{{Name: "root", Provider: "test", Model: "mock-model"}}, "root"),
		ToolsetInfo(0, false, "root"),
		UserMessage("Hi there", sess.ID),
		StreamStarted(sess.ID, "root"),
		AgentChoiceReasoning("root", "The user wants a greeting"),
		AgentChoice("root", "Hello!"),
		AgentChoiceReasoning("root", " I should be friendly"),
		AgentChoice("root", " How can I help you today?"),
		MessageAdded(sess.ID, msgAdded.Message, "root"),
		TokenUsageWithMessage(sess.ID, "root", 15, 20, 35, 0, 0, &MessageUsage{
			Usage: chat.Usage{InputTokens: 15, OutputTokens: 20},
			Model: "test/mock-model",
		}),
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

	sess := session.New(session.WithUserMessage("Please use the test tool"))

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

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Hi"))
	sess.Title = "Unit Test"

	evCh := rt.RunStream(t.Context(), sess)

	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}

	require.Len(t, events, 7)
	require.IsType(t, &AgentInfoEvent{}, events[0])
	require.IsType(t, &TeamInfoEvent{}, events[1])
	require.IsType(t, &ToolsetInfoEvent{}, events[2])
	require.IsType(t, &UserMessageEvent{}, events[3])
	require.IsType(t, &StreamStartedEvent{}, events[4])
	require.IsType(t, &ErrorEvent{}, events[5])
	require.IsType(t, &StreamStoppedEvent{}, events[6])

	errorEvent := events[5].(*ErrorEvent)
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

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Hi"))
	sess.Title = "Unit Test"

	ctx, cancel := context.WithCancel(t.Context())
	evCh := rt.RunStream(ctx, sess)

	cancel()

	var events []Event
	for ev := range evCh {
		events = append(events, ev)
	}

	require.GreaterOrEqual(t, len(events), 5)
	require.IsType(t, &AgentInfoEvent{}, events[0])
	require.IsType(t, &TeamInfoEvent{}, events[1])
	require.IsType(t, &ToolsetInfoEvent{}, events[2])
	require.IsType(t, &UserMessageEvent{}, events[3])
	require.IsType(t, &StreamStartedEvent{}, events[4])
	require.IsType(t, &StreamStoppedEvent{}, events[len(events)-1])
}

// stubRAGStrategy is a minimal implementation of strategy.Strategy for testing RAG initialization.
type stubRAGStrategy struct{}

func (s *stubRAGStrategy) Initialize(_ context.Context, _ []string, _ strategy.ChunkingConfig) error {
	return nil
}

func (s *stubRAGStrategy) Query(_ context.Context, _ string, _ int, _ float64) ([]database.SearchResult, error) {
	return nil, nil
}

func (s *stubRAGStrategy) CheckAndReindexChangedFiles(_ context.Context, _ []string, _ strategy.ChunkingConfig) error {
	return nil
}

func (s *stubRAGStrategy) StartFileWatcher(_ context.Context, _ []string, _ strategy.ChunkingConfig) error {
	return nil
}

func (s *stubRAGStrategy) Close() error { return nil }

func TestStartBackgroundRAGInit_StopsForwardingAfterContextCancel(t *testing.T) {
	t.Parallel()

	baseCtx := t.Context()
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	// Build a RAG manager with a stub strategy and a controllable event channel.
	strategyEvents := make(chan ragtypes.Event, 10)
	mgr, err := rag.New(
		ctx,
		"test-rag",
		rag.Config{
			StrategyConfigs: []strategy.Config{
				{
					Name:     "stub",
					Strategy: &stubRAGStrategy{},
					Docs:     nil,
				},
			},
		},
		strategyEvents,
	)
	require.NoError(t, err)
	defer func() {
		_ = mgr.Close()
	}()

	tm := team.New(team.WithRAGManagers(map[string]*rag.Manager{
		"default": mgr,
	}))

	rt := &LocalRuntime{
		team:         tm,
		currentAgent: "root",
	}

	eventsCh := make(chan Event, 10)

	// Start background RAG init with event forwarding.
	rt.StartBackgroundRAGInit(ctx, func(ev Event) {
		eventsCh <- ev
	})

	// Emit an "indexing_completed" event and ensure it is forwarded.
	strategyEvents <- ragtypes.Event{
		Type:         ragtypes.EventTypeIndexingComplete,
		StrategyName: "stub",
	}

	select {
	case <-eventsCh:
		// ok: at least one event forwarded
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected RAG event to be forwarded before cancellation")
	}

	// Cancel the context and ensure no further events are forwarded.
	cancel()

	// Give the forwarder time to observe cancellation.
	time.Sleep(10 * time.Millisecond)

	// Emit another event; it should NOT be forwarded.
	strategyEvents <- ragtypes.Event{
		Type:         ragtypes.EventTypeIndexingComplete,
		StrategyName: "stub",
	}

	select {
	case ev := <-eventsCh:
		t.Fatalf("expected no events after cancellation, got %T", ev)
	case <-time.After(20 * time.Millisecond):
		// success: no events forwarded
	}
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
			sess := session.New(session.WithUserMessage("Use tools"))
			events := runSession(t, sess, stream)

			require.True(t, hasEventType(t, events, &PartialToolCallEvent{}), "Expected PartialToolCallEvent for %s", tt.description)
			require.True(t, hasEventType(t, events, &StreamStartedEvent{}), "Expected StreamStartedEvent")
			require.True(t, hasEventType(t, events, &StreamStoppedEvent{}), "Expected StreamStoppedEvent")
		})
	}
}

// queueProvider returns a different stream on each CreateChatCompletionStream call.
type queueProvider struct {
	id      string
	mu      sync.Mutex
	streams []chat.MessageStream
}

func (p *queueProvider) ID() string { return p.id }

func (p *queueProvider) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.streams) == 0 {
		return &mockStream{}, nil
	}
	s := p.streams[0]
	p.streams = p.streams[1:]
	return s, nil
}

func (p *queueProvider) BaseConfig() base.Config { return base.Config{} }

func (p *queueProvider) MaxTokens() int { return 0 }

type mockModelStoreWithLimit struct{ limit int }

func (m mockModelStoreWithLimit) GetModel(context.Context, string) (*modelsdev.Model, error) {
	return &modelsdev.Model{Limit: modelsdev.Limit{Context: m.limit}, Cost: &modelsdev.Cost{}}, nil
}

func TestCompaction(t *testing.T) {
	// First stream: assistant issues a tool call and usage exceeds 90% threshold
	mainStream := newStreamBuilder().
		AddContent("Hello there").
		AddStopWithUsage(101, 0). // Context limit will be 100
		Build()

	// Second stream: summary generation (simple content)
	summaryStream := newStreamBuilder().
		AddContent("summary").
		AddStopWithUsage(1, 1).
		Build()

	prov := &queueProvider{id: "test/mock-model", streams: []chat.MessageStream{mainStream, summaryStream}}

	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	// Enable compaction and provide a model store with context limit = 100
	rt, err := NewLocalRuntime(tm, WithSessionCompaction(true), WithModelStore(mockModelStoreWithLimit{limit: 100}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Start"))
	e := rt.RunStream(t.Context(), sess)
	for range e {
	}
	sess.AddMessage(session.UserMessage("Again"))
	events := rt.RunStream(t.Context(), sess)

	var seen []Event
	for ev := range events {
		seen = append(seen, ev)
	}

	compactionStartIdx := -1
	for i, ev := range seen {
		if e, ok := ev.(*SessionCompactionEvent); ok {
			if e.Status == "started" && compactionStartIdx == -1 {
				compactionStartIdx = i
			}
		}
	}

	require.NotEqual(t, -1, compactionStartIdx, "expected a SessionCompaction start event")
}

func TestSessionWithoutUserMessage(t *testing.T) {
	stream := newStreamBuilder().AddContent("OK").AddStopWithUsage(1, 1).Build()

	sess := session.New(
		session.WithSendUserMessage(false),
	)

	events := runSession(t, sess, stream)

	require.True(t, hasEventType(t, events, &StreamStartedEvent{}), "Expected StreamStartedEvent")
	require.True(t, hasEventType(t, events, &StreamStoppedEvent{}), "Expected StreamStoppedEvent")
	require.False(t, hasEventType(t, events, &UserMessageEvent{}), "Should not have UserMessageEvent when SendUserMessage is false")
}

// --- Tool setup failure handling tests ---

func collectEvents(ch chan Event) []Event {
	n := len(ch)
	evs := make([]Event, 0, n)
	for range n {
		evs = append(evs, <-ch)
	}
	return evs
}

func hasWarningEvent(evs []Event) bool {
	for _, e := range evs {
		if _, ok := e.(*WarningEvent); ok {
			return true
		}
	}
	return false
}

func TestGetTools_WarningHandling(t *testing.T) {
	tests := []struct {
		name          string
		toolsets      []tools.ToolSet
		wantToolCount int
		wantWarning   bool
	}{
		{
			name:          "partial success warns once",
			toolsets:      []tools.ToolSet{newStubToolSet(nil, []tools.Tool{{Name: "good", Parameters: map[string]any{}}}, nil), newStubToolSet(errors.New("boom"), nil, nil)},
			wantToolCount: 1,
			wantWarning:   true,
		},
		{
			name:          "all fail on start warns once",
			toolsets:      []tools.ToolSet{newStubToolSet(errors.New("s1"), nil, nil), newStubToolSet(errors.New("s2"), nil, nil)},
			wantToolCount: 0,
			wantWarning:   true,
		},
		{
			name:          "list failure warns once",
			toolsets:      []tools.ToolSet{newStubToolSet(nil, nil, errors.New("boom"))},
			wantToolCount: 0,
			wantWarning:   true,
		},
		{
			name:          "no toolsets no warning",
			toolsets:      nil,
			wantToolCount: 0,
			wantWarning:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := agent.New("root", "test", agent.WithToolSets(tt.toolsets...), agent.WithModel(&mockProvider{}))
			tm := team.New(team.WithAgents(root))
			rt, err := NewLocalRuntime(tm, WithModelStore(mockModelStore{}))
			require.NoError(t, err)

			events := make(chan Event, 10)
			sessionSpan := trace.SpanFromContext(t.Context())

			// First call
			tools1, err := rt.getTools(t.Context(), root, sessionSpan, events)
			require.NoError(t, err)
			require.Len(t, tools1, tt.wantToolCount)

			rt.emitAgentWarnings(root, events)
			evs := collectEvents(events)
			require.Equal(t, tt.wantWarning, hasWarningEvent(evs), "warning event mismatch on first call")
		})
	}
}

func TestNewRuntime_NoAgentsError(t *testing.T) {
	tm := team.New()

	_, err := New(tm, WithModelStore(mockModelStore{}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no agents loaded")
}

func TestNewRuntime_InvalidCurrentAgentError(t *testing.T) {
	root := agent.New("root", "You are a test agent")
	tm := team.New(team.WithAgents(root))

	// Ask for a non-existent current agent
	_, err := New(tm, WithCurrentAgent("other"), WithModelStore(mockModelStore{}))
	require.Contains(t, err.Error(), "agent not found: other (available agents: root)")
}

func TestSummarize_EmptySession(t *testing.T) {
	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New()
	sess.Title = "Empty Session Test"

	// Try to summarize the empty session
	events := make(chan Event, 10)
	rt.Summarize(t.Context(), sess, "", events)
	close(events)

	// Collect events
	var warningFound bool
	var warningMsg string
	for ev := range events {
		if warningEvent, ok := ev.(*WarningEvent); ok {
			warningFound = true
			warningMsg = warningEvent.Message
		}
	}

	// Should have received a warning event about empty session
	require.True(t, warningFound, "expected a warning event for empty session")
	require.Contains(t, warningMsg, "empty", "warning message should mention empty session")
}

func TestProcessToolCalls_UnknownTool_NoToolResultMessage(t *testing.T) {
	// Build a runtime with a simple agent but no tools registered matching the call
	root := agent.New("root", "You are a test agent", agent.WithModel(&mockProvider{}))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Register default tools (contains only transfer_task) to ensure unknown tool isn't matched
	rt.registerDefaultTools()

	sess := session.New(session.WithUserMessage("Start"))

	// Simulate a model-issued tool call to a non-existent tool
	calls := []tools.ToolCall{{
		ID:       "tool-unknown-1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "non_existent_tool", Arguments: "{}"},
	}}

	events := make(chan Event, 10)

	// No agentTools provided and runtime toolMap doesn't have this tool name
	rt.processToolCalls(t.Context(), sess, calls, nil, events)

	// Drain events channel
	close(events)
	for range events {
	}

	var sawToolMsg bool
	for _, it := range sess.Messages {
		if it.IsMessage() && it.Message.Message.Role == chat.MessageRoleTool && it.Message.Message.ToolCallID == "tool-unknown-1" {
			sawToolMsg = true
			break
		}
	}
	require.False(t, sawToolMsg, "no tool result should be added for unknown tool; this reproduces invalid sequencing state")
}

func TestEmitStartupInfo(t *testing.T) {
	// Create a simple agent with mock provider
	prov := &mockProvider{id: "test/startup-model", stream: &mockStream{}}
	root := agent.New("startup-test-agent", "You are a startup test agent",
		agent.WithModel(prov),
		agent.WithDescription("This is a startup test agent"),
		agent.WithWelcomeMessage("Welcome!"),
	)
	other := agent.New("other-agent", "You are another agent",
		agent.WithModel(prov),
		agent.WithDescription("This is another agent"),
	)
	tm := team.New(team.WithAgents(root, other))

	rt, err := NewLocalRuntime(tm, WithCurrentAgent("startup-test-agent"), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Create a channel to collect events
	events := make(chan Event, 10)

	// Call EmitStartupInfo
	rt.EmitStartupInfo(t.Context(), events)
	close(events)

	// Collect events
	var collectedEvents []Event
	for event := range events {
		collectedEvents = append(collectedEvents, event)
	}

	// Verify expected events are emitted
	expectedEvents := []Event{
		AgentInfo("startup-test-agent", "test/startup-model", "This is a startup test agent", "Welcome!"),
		TeamInfo([]AgentDetails{
			{Name: "startup-test-agent", Description: "This is a startup test agent", Provider: "test", Model: "startup-model"},
			{Name: "other-agent", Description: "This is another agent", Provider: "test", Model: "startup-model"},
		}, "startup-test-agent"),
		ToolsetInfo(0, false, "startup-test-agent"), // No tools configured
	}

	require.Equal(t, expectedEvents, collectedEvents)

	// Test that calling EmitStartupInfo again doesn't emit duplicate events
	events2 := make(chan Event, 10)
	rt.EmitStartupInfo(t.Context(), events2)
	close(events2)

	var collectedEvents2 []Event
	for event := range events2 {
		collectedEvents2 = append(collectedEvents2, event)
	}

	// Should be empty due to deduplication
	require.Empty(t, collectedEvents2, "EmitStartupInfo should not emit duplicate events")
}

func TestPermissions_DenyBlocksToolExecution(t *testing.T) {
	// Test that tools matching deny patterns are blocked
	permChecker := permissions.NewChecker(&latest.PermissionsConfig{
		Deny: []string{"dangerous_tool"},
	})

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(
		team.WithAgents(root),
		team.WithPermissions(permChecker),
	)

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Test"))

	// Create a tool call for the denied tool
	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "dangerous_tool", Arguments: "{}"},
	}}

	// Define a tool that exists
	agentTools := []tools.Tool{{
		Name:       "dangerous_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("executed"), nil
		},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	// The tool should be denied, look for a ToolCallResponseEvent with error
	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if tr, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = tr
			break
		}
	}

	require.NotNil(t, toolResponse, "expected ToolCallResponseEvent")
	require.Contains(t, toolResponse.Response, "denied by permissions")
}

func TestPermissions_AllowAutoApprovesTool(t *testing.T) {
	// Test that tools matching allow patterns are auto-approved without --yolo
	permChecker := permissions.NewChecker(&latest.PermissionsConfig{
		Allow: []string{"safe_*"},
	})

	var executed bool
	agentTools := []tools.Tool{{
		Name:       "safe_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			executed = true
			return tools.ResultSuccess("executed"), nil
		},
	}}

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithToolSets(newStubToolSet(nil, agentTools, nil)),
	)
	tm := team.New(
		team.WithAgents(root),
		team.WithPermissions(permChecker),
	)

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Test"))
	// Note: ToolsApproved is false (no --yolo)
	require.False(t, sess.ToolsApproved)

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "safe_tool", Arguments: "{}"},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	// The tool should have been executed due to allow pattern
	require.True(t, executed, "expected tool to be auto-approved and executed")
}

func TestPermissions_DenyTakesPriorityOverAllow(t *testing.T) {
	// Test that deny patterns take priority over allow patterns
	permChecker := permissions.NewChecker(&latest.PermissionsConfig{
		Allow: []string{"*"}, // Allow everything
		Deny:  []string{"forbidden_tool"},
	})

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(
		team.WithAgents(root),
		team.WithPermissions(permChecker),
	)

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Test"))

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "forbidden_tool", Arguments: "{}"},
	}}

	agentTools := []tools.Tool{{
		Name:       "forbidden_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("executed"), nil
		},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	// The tool should be denied despite wildcard allow
	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if tr, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = tr
			break
		}
	}

	require.NotNil(t, toolResponse, "expected ToolCallResponseEvent")
	require.Contains(t, toolResponse.Response, "denied by permissions")
}

func TestSessionPermissions_DenyBlocksToolExecution(t *testing.T) {
	// Test that session-level deny patterns block tools
	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Create session with permissions that deny the tool
	sess := session.New(
		session.WithUserMessage("Test"),
		session.WithPermissions(&session.PermissionsConfig{
			Deny: []string{"blocked_tool"},
		}),
	)

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "blocked_tool", Arguments: "{}"},
	}}

	agentTools := []tools.Tool{{
		Name:       "blocked_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("executed"), nil
		},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if tr, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = tr
			break
		}
	}

	require.NotNil(t, toolResponse, "expected ToolCallResponseEvent")
	require.Contains(t, toolResponse.Response, "denied by session permissions")
}

func TestSessionPermissions_AllowAutoApprovesTool(t *testing.T) {
	// Test that session-level allow patterns auto-approve tools
	var executed bool
	agentTools := []tools.Tool{{
		Name:       "allowed_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			executed = true
			return tools.ResultSuccess("executed"), nil
		},
	}}

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithToolSets(newStubToolSet(nil, agentTools, nil)),
	)
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Create session with permissions that allow the tool
	sess := session.New(
		session.WithUserMessage("Test"),
		session.WithPermissions(&session.PermissionsConfig{
			Allow: []string{"allowed_*"},
		}),
	)
	require.False(t, sess.ToolsApproved) // No --yolo

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "allowed_tool", Arguments: "{}"},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	require.True(t, executed, "expected tool to be auto-approved by session permissions")
}

func TestSessionPermissions_TakePriorityOverTeamPermissions(t *testing.T) {
	// Test that session permissions are evaluated before team permissions
	// Team allows everything, but session denies specific tool
	teamPermChecker := permissions.NewChecker(&latest.PermissionsConfig{
		Allow: []string{"*"}, // Team allows all
	})

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(
		team.WithAgents(root),
		team.WithPermissions(teamPermChecker),
	)

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Session denies the tool (should override team allow)
	sess := session.New(
		session.WithUserMessage("Test"),
		session.WithPermissions(&session.PermissionsConfig{
			Deny: []string{"overridden_tool"},
		}),
	)

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "overridden_tool", Arguments: "{}"},
	}}

	agentTools := []tools.Tool{{
		Name:       "overridden_tool",
		Parameters: map[string]any{},
		Handler: func(ctx context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("executed"), nil
		},
	}}

	events := make(chan Event, 10)
	rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
	close(events)

	// Session deny should take priority over team allow
	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if tr, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = tr
			break
		}
	}

	require.NotNil(t, toolResponse, "expected ToolCallResponseEvent")
	require.Contains(t, toolResponse.Response, "denied by session permissions")
}

func TestToolRejectionWithReason(t *testing.T) {
	// Test that rejection reasons are included in the tool error response
	agentTools := []tools.Tool{{
		Name:       "shell",
		Parameters: map[string]any{},
		Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
			t.Fatal("tool should not be executed when rejected")
			return nil, nil
		},
	}}

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithToolSets(newStubToolSet(nil, agentTools, nil)),
	)
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Test"))
	require.False(t, sess.ToolsApproved) // No --yolo

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "shell", Arguments: "{}"},
	}}

	events := make(chan Event, 10)

	// Run in goroutine since it will block waiting for confirmation
	go func() {
		rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
		close(events)
	}()

	// Wait for confirmation request and then reject with a reason
	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if _, ok := ev.(*ToolCallConfirmationEvent); ok {
			// Send rejection with a specific reason
			rt.resumeChan <- ResumeReject("The arguments provided are incorrect.")
		}
		if resp, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = resp
		}
	}

	require.NotNil(t, toolResponse, "expected a tool response event")
	require.True(t, toolResponse.Result.IsError, "expected tool result to be an error")
	require.Contains(t, toolResponse.Response, "The user rejected the tool call.")
	require.Contains(t, toolResponse.Response, "Reason: The arguments provided are incorrect.")
}

func TestToolRejectionWithoutReason(t *testing.T) {
	// Test that rejection without a reason still works
	agentTools := []tools.Tool{{
		Name:       "shell",
		Parameters: map[string]any{},
		Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
			t.Fatal("tool should not be executed when rejected")
			return nil, nil
		},
	}}

	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithToolSets(newStubToolSet(nil, agentTools, nil)),
	)
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Test"))
	require.False(t, sess.ToolsApproved) // No --yolo

	calls := []tools.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: tools.FunctionCall{Name: "shell", Arguments: "{}"},
	}}

	events := make(chan Event, 10)

	// Run in goroutine since it will block waiting for confirmation
	go func() {
		rt.processToolCalls(t.Context(), sess, calls, agentTools, events)
		close(events)
	}()

	// Wait for confirmation request and then reject without a reason
	var toolResponse *ToolCallResponseEvent
	for ev := range events {
		if _, ok := ev.(*ToolCallConfirmationEvent); ok {
			// Send rejection without a reason
			rt.resumeChan <- ResumeReject("")
		}
		if resp, ok := ev.(*ToolCallResponseEvent); ok {
			toolResponse = resp
		}
	}

	require.NotNil(t, toolResponse, "expected a tool response event")
	require.True(t, toolResponse.Result.IsError, "expected tool result to be an error")
	require.Equal(t, "The user rejected the tool call.", toolResponse.Response)
	require.NotContains(t, toolResponse.Response, "Reason:")
}
