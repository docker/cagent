package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

// multiStreamProvider returns different streams on consecutive calls.
type multiStreamProvider struct {
	id      string
	mu      sync.Mutex
	streams []chat.MessageStream
	idx     int
}

func (m *multiStreamProvider) ID() string { return m.id }

func (m *multiStreamProvider) CreateChatCompletionStream(_ context.Context, _ []chat.Message, _ []tools.Tool) (chat.MessageStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.streams) {
		return m.streams[len(m.streams)-1], nil
	}
	s := m.streams[m.idx]
	m.idx++
	return s, nil
}

func (m *multiStreamProvider) BaseConfig() base.Config { return base.Config{} }

func (m *multiStreamProvider) MaxTokens() int { return 0 }

func TestPersistentRuntime_SubAgentMessagesNotPersistedToParent(t *testing.T) {
	// Stream 1 (root): produces a transfer_task tool call to "worker"
	rootStream := newStreamBuilder().
		AddToolCallName("call_transfer", "transfer_task").
		AddToolCallArguments("call_transfer", `{"agent":"worker","task":"do work","expected_output":"result"}`).
		AddStopWithUsage(10, 5).
		Build()

	// Stream 2 (worker sub-agent): produces streaming content simulating work
	workerStream := newStreamBuilder().
		AddContent("I am doing ").
		AddContent("the work now.").
		AddStopWithUsage(5, 10).
		Build()

	prov := &multiStreamProvider{
		id:      "test/mock-model",
		streams: []chat.MessageStream{rootStream, workerStream},
	}

	worker := agent.New("worker", "Worker agent", agent.WithModel(prov))
	root := agent.New("root", "Root coordinator",
		agent.WithModel(prov),
		agent.WithToolSets(builtin.NewTransferTaskTool()),
	)
	agent.WithSubAgents(worker)(root)

	tm := team.New(team.WithAgents(root, worker))

	store := session.NewInMemorySessionStore()

	rt, err := New(tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
		WithSessionStore(store),
	)
	require.NoError(t, err)

	sess := session.New(
		session.WithUserMessage("Please delegate work to the worker"),
		session.WithToolsApproved(true),
	)
	sess.Title = "Test Transfer Persistence"

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	evCh := rt.RunStream(t.Context(), sess)
	for range evCh {
	}

	parentSess, err := store.GetSession(t.Context(), sess.ID)
	require.NoError(t, err)

	// Verify no sub-agent messages leaked into the parent session
	for _, item := range parentSess.Messages {
		if !item.IsMessage() {
			continue
		}
		assert.NotEqual(t, "worker", item.Message.AgentName,
			"Sub-agent 'worker' messages should not be in the parent session. "+
				"Found message with role=%s content=%q",
			item.Message.Message.Role, item.Message.Message.Content)
	}

	// Verify the sub-session was persisted and contains the worker's messages
	var subSess *session.Session
	for _, item := range parentSess.Messages {
		if item.IsSubSession() {
			subSess = item.SubSession
			break
		}
	}
	require.NotNil(t, subSess,
		"Sub-session should be persisted in the parent session")

	var workerMsgCount int
	for _, item := range subSess.Messages {
		if item.IsMessage() && item.Message.AgentName == "worker" {
			workerMsgCount++
		}
	}
	assert.Positive(t, workerMsgCount,
		"Worker messages should be in the sub-session")

	// Verify the root agent's assistant message (with transfer_task tool call)
	// and the tool result are both persisted in the parent
	var roles []chat.MessageRole
	for _, item := range parentSess.Messages {
		if item.IsMessage() {
			roles = append(roles, item.Message.Message.Role)
		}
	}
	assert.Contains(t, roles, chat.MessageRoleAssistant,
		"Parent session should contain root's assistant message with the transfer_task tool call")
	assert.Contains(t, roles, chat.MessageRoleTool,
		"Parent session should contain the tool result for transfer_task")
}
