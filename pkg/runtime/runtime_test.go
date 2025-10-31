package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type stubToolSet struct {
	startErr     error
	tools        []tools.Tool
	listErr      error
	instructions string
}

func newStubToolSet(startErr error, toolsList []tools.Tool, listErr error) tools.ToolSet {
	return &stubToolSet{
		startErr:     startErr,
		tools:        toolsList,
		listErr:      listErr,
		instructions: "stub",
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
func (s *stubToolSet) Instructions() string                           { return s.instructions }
func (s *stubToolSet) SetElicitationHandler(tools.ElicitationHandler) {}
func (s *stubToolSet) SetOAuthSuccessHandler(func())                  {}

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

func (m *mockProvider) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return m.stream, nil
}

func (m *mockProvider) Options() options.ModelOptions { return options.ModelOptions{} }

func (m *mockProvider) MaxTokens() int { return 0 }

type mockProviderWithError struct {
	id string
}

func (m *mockProviderWithError) ID() string { return m.id }

func (m *mockProviderWithError) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return nil, fmt.Errorf("simulated error creating chat completion stream")
}

func (m *mockProviderWithError) Options() options.ModelOptions { return options.ModelOptions{} }

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

func assertTokenUsageEvent(t *testing.T, ev Event, sess *session.Session, input, output int) {
	t.Helper()

	usageEvent, ok := ev.(*TokenUsageEvent)
	require.True(t, ok, "expected TokenUsageEvent, got %T", ev)
	require.NotNil(t, usageEvent.Usage)

	require.Equal(t, input, usageEvent.Usage.InputTokens)
	require.Equal(t, output, usageEvent.Usage.OutputTokens)
	require.Equal(t, input+output, usageEvent.Usage.ContextLength)
	require.InDelta(t, 0.0, usageEvent.Usage.Cost, 1e-9)

	require.NotNil(t, usageEvent.Usage.Breakdown)
	require.Len(t, usageEvent.Usage.Breakdown, 1)

	row := usageEvent.Usage.Breakdown[0]
	require.Equal(t, sess.ID, row.SessionID)
	require.Equal(t, "root", row.AgentName)
	require.Equal(t, sess.Title, row.Title)
	require.Equal(t, 0, row.Depth)
	require.Equal(t, input, row.InputTokens)
	require.Equal(t, output, row.OutputTokens)
	require.InDelta(t, 0.0, row.Cost, 1e-9)
	require.True(t, row.Active)

	require.ElementsMatch(t, []string{sess.ID}, usageEvent.Usage.ActiveSessions)
}

func TestSimple(t *testing.T) {
	stream := newStreamBuilder().
		AddContent("Hello").
		AddStopWithUsage(3, 2).
		Build()

	sess := session.New(session.WithUserMessage("", "Hi"))

	events := runSession(t, sess, stream)

	require.Len(t, events, 5)

	require.IsType(t, &UserMessageEvent{}, events[0])
	require.Equal(t, "Hi", events[0].(*UserMessageEvent).Message)

	require.IsType(t, &StreamStartedEvent{}, events[1])
	require.Equal(t, sess.ID, events[1].(*StreamStartedEvent).SessionID)

	require.IsType(t, &AgentChoiceEvent{}, events[2])
	require.Equal(t, "Hello", events[2].(*AgentChoiceEvent).Content)

	assertTokenUsageEvent(t, events[3], sess, 3, 2)

	require.IsType(t, &StreamStoppedEvent{}, events[4])
	require.Equal(t, sess.ID, events[4].(*StreamStoppedEvent).SessionID)
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

	require.Len(t, events, 8)

	require.IsType(t, &UserMessageEvent{}, events[0])
	require.Equal(t, "Please greet me", events[0].(*UserMessageEvent).Message)

	require.IsType(t, &StreamStartedEvent{}, events[1])

	require.IsType(t, &AgentChoiceEvent{}, events[2])
	require.Equal(t, "Hello ", events[2].(*AgentChoiceEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[3])
	require.Equal(t, "there, ", events[3].(*AgentChoiceEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[4])
	require.Equal(t, "how ", events[4].(*AgentChoiceEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[5])
	require.Equal(t, "are ", events[5].(*AgentChoiceEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[6])
	require.Equal(t, "you?", events[6].(*AgentChoiceEvent).Content)

	assertTokenUsageEvent(t, events[7], sess, 8, 12)
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

	require.Len(t, events, 7)

	require.IsType(t, &UserMessageEvent{}, events[0])
	require.Equal(t, "Hi", events[0].(*UserMessageEvent).Message)

	require.IsType(t, &StreamStartedEvent{}, events[1])

	require.IsType(t, &AgentChoiceReasoningEvent{}, events[2])
	require.Equal(t, "Let me think about this...", events[2].(*AgentChoiceReasoningEvent).Content)

	require.IsType(t, &AgentChoiceReasoningEvent{}, events[3])
	require.Equal(t, " I should respond politely.", events[3].(*AgentChoiceReasoningEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[4])
	require.Equal(t, "Hello, how can I help you?", events[4].(*AgentChoiceEvent).Content)

	assertTokenUsageEvent(t, events[5], sess, 10, 15)

	require.IsType(t, &StreamStoppedEvent{}, events[6])
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

	require.Len(t, events, 8)

	require.IsType(t, &UserMessageEvent{}, events[0])
	require.Equal(t, "Hi there", events[0].(*UserMessageEvent).Message)

	require.IsType(t, &StreamStartedEvent{}, events[1])

	require.IsType(t, &AgentChoiceReasoningEvent{}, events[2])
	require.Equal(t, "The user wants a greeting", events[2].(*AgentChoiceReasoningEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[3])
	require.Equal(t, "Hello!", events[3].(*AgentChoiceEvent).Content)

	require.IsType(t, &AgentChoiceReasoningEvent{}, events[4])
	require.Equal(t, " I should be friendly", events[4].(*AgentChoiceReasoningEvent).Content)

	require.IsType(t, &AgentChoiceEvent{}, events[5])
	require.Equal(t, " How can I help you today?", events[5].(*AgentChoiceEvent).Content)

	assertTokenUsageEvent(t, events[6], sess, 15, 20)

	require.IsType(t, &StreamStoppedEvent{}, events[7])
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

	ctx, cancel := context.WithCancel(t.Context())
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

// queueProvider returns a different stream on each CreateChatCompletionStream call.
type queueProvider struct {
	id      string
	streams []chat.MessageStream
}

func (p *queueProvider) ID() string { return p.id }

func (p *queueProvider) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	if len(p.streams) == 0 {
		return &mockStream{}, nil
	}
	s := p.streams[0]
	p.streams = p.streams[1:]
	return s, nil
}

func (p *queueProvider) Options() options.ModelOptions { return options.ModelOptions{} }

func (p *queueProvider) MaxTokens() int { return 0 }

type mockModelStoreWithLimit struct{ limit int }

func (m mockModelStoreWithLimit) GetModel(context.Context, string) (*modelsdev.Model, error) {
	return &modelsdev.Model{Limit: modelsdev.Limit{Context: m.limit}, Cost: &modelsdev.Cost{}}, nil
}

func TestCompactionOccursAfterToolResultsWhenToolUsePresent(t *testing.T) {
	// First stream: assistant issues a tool call and usage exceeds 90% threshold
	mainStream := newStreamBuilder().
		AddToolCallName("call_1", "test_tool").
		AddToolCallArguments("call_1", "{}").
		AddStopWithUsage(95, 0). // Context limit will be 100
		Build()

	// Second stream: summary generation (simple content)
	summaryStream := newStreamBuilder().
		AddContent("summary").
		AddStopWithUsage(1, 1).
		Build()

	prov := &queueProvider{id: "test/mock-model", streams: []chat.MessageStream{mainStream, summaryStream}}

	// Provide an agent tool that will satisfy the tool call without requiring approvals
	testTool := tools.Tool{
		Name:        "test_tool",
		Description: "test",
		Parameters:  map[string]any{},
		Annotations: tools.ToolAnnotations{ReadOnlyHint: true},
		Handler: func(ctx context.Context, call tools.ToolCall) (*tools.ToolCallResult, error) {
			return &tools.ToolCallResult{Output: "ok"}, nil
		},
	}

	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithTools(testTool),
	)
	tm := team.New(team.WithAgents(root))

	// Enable compaction and provide a model store with context limit = 100
	rt, err := New(tm, WithSessionCompaction(true), WithModelStore(mockModelStoreWithLimit{limit: 100}))
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "Start"))
	events := rt.RunStream(t.Context(), sess)

	// Collect events
	var seen []Event
	for ev := range events {
		seen = append(seen, ev)
	}

	// Find indices of ToolCallResponse and compaction start (from RunStream)
	toolRespIdx := -1
	compactionStartIdx := -1
	for i, ev := range seen {
		switch e := ev.(type) {
		case *ToolCallResponseEvent:
			if toolRespIdx == -1 {
				toolRespIdx = i
			}
		case *SessionCompactionEvent:
			// We only want the RunStream-level "start" status (not Summarize's "started")
			if e.Status == "start" && compactionStartIdx == -1 {
				compactionStartIdx = i
			}
		}
	}

	require.NotEqual(t, -1, toolRespIdx, "expected a ToolCallResponseEvent")
	require.NotEqual(t, -1, compactionStartIdx, "expected a SessionCompaction start event")

	// Assert compaction is triggered only after tool results have been appended
	require.Greater(t, compactionStartIdx, toolRespIdx, "compaction should occur after tool results when tool_use is present")
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
			root := agent.New("root", "test", agent.WithToolSets(tt.toolsets...))
			tm := team.New(team.WithAgents(root))
			rt, err := New(tm, WithModelStore(mockModelStore{}))
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
	// Create a team with a single agent named "root"
	root := agent.New("root", "You are a test agent")
	tm := team.New(team.WithAgents(root))

	// Ask for a non-existent current agent
	_, err := New(tm, WithCurrentAgent("other"), WithModelStore(mockModelStore{}))
	require.Contains(t, err.Error(), "agent not found: other (available agents: root)")
}

func TestSummarize_EmptySession(t *testing.T) {
	// Create a runtime with a simple agent
	prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Create an empty session (no messages)
	sess := session.New()
	sess.Title = "Empty Session Test"

	// Try to summarize the empty session
	events := make(chan Event, 10)
	rt.Summarize(t.Context(), sess, events)
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
	root := agent.New("root", "You are a test agent")
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	// Register default tools (contains only transfer_task) to ensure unknown tool isn't matched
	rt.registerDefaultTools()

	sess := session.New(session.WithUserMessage("", "Start"))

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

	// Verify no tool result message was added for the unknown tool
	var sawToolMsg bool
	for _, it := range sess.Messages {
		if it.IsMessage() && it.Message.Message.Role == chat.MessageRoleTool && it.Message.Message.ToolCallID == "tool-unknown-1" {
			sawToolMsg = true
			break
		}
	}
	require.False(t, sawToolMsg, "no tool result should be added for unknown tool; this reproduces invalid sequencing state")
}
