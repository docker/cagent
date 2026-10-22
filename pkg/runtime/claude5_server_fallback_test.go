package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider/anthropic/anthropictest"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// assistantBlocks returns the content block types of each assistant message
// in a captured request, with tool_use IDs and thinking signatures appended.
func assistantBlocks(t *testing.T, req anthropictest.Request) [][]string {
	t.Helper()
	messages, _ := req.Body["messages"].([]any)
	var turns [][]string
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		content, _ := msg["content"].([]any)
		var types []string
		for _, c := range content {
			block, _ := c.(map[string]any)
			typ, _ := block["type"].(string)
			switch typ {
			case "tool_use":
				typ += ":" + block["id"].(string)
			case "thinking":
				typ += ":" + block["signature"].(string)
			}
			types = append(types, typ)
		}
		turns = append(turns, types)
	}
	return turns
}

// A server fallback mid-turn hands the turn to another model: the declined
// model's tool call never runs, the stored turn names the model that served
// it, and every later request, before and after a reload, replays the turn
// as the docs' continuation table requires. A later sticky-routed turn is
// attributed to the fallback model without any marker.
func TestServerFallbackLifecycle(t *testing.T) {
	t.Parallel()

	const primary, fallback = "claude-opus-4-8", "claude-sonnet-4-6"
	server := anthropictest.NewServer(t,
		anthropictest.FallbackMidOutputReply("msg_1", primary, fallback, "before ", "after", "t-declined"),
		anthropictest.StickyReply("msg_2", fallback, "sticky"),
		anthropictest.TextReply("msg_3", "done", 10),
	)
	var calls atomic.Int32
	record := agent.WithTools(tools.Tool{Name: "record", Parameters: map[string]any{"type": "object"}, Handler: func(context.Context, tools.ToolCall, tools.Runtime) (*tools.ToolCallResult, error) {
		calls.Add(1)
		return tools.ResultSuccess("recorded"), nil
	}})
	opts := map[string]any{"fallbacks": []any{fallback}}
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	models := catalogStore{costs: map[string]modelsdev.Cost{
		"anthropic/" + primary:  {Input: 10, Output: 50},
		"anthropic/" + fallback: {Input: 3, Output: 15},
	}}
	rt := anthropicRuntime(t, server, primary, opts, openSessionStore(t, dbPath), models, record)

	sess := session.New(session.WithUserMessage("u1"), session.WithToolsApproved(true))
	requireNoErrorEvents(t, drainRunStream(t, rt, sess))
	assert.Zero(t, calls.Load(), "the declined model's tool call must never run")

	sess.AddMessage(session.UserMessage("u2"))
	requireNoErrorEvents(t, drainRunStream(t, rt, sess))

	loaded, err := openSessionStore(t, dbPath).GetSession(t.Context(), sess.ID)
	require.NoError(t, err)
	var served []string
	var costs []float64
	for _, item := range loaded.Messages {
		if item.Message != nil && item.Message.Message.Role == chat.MessageRoleAssistant {
			served = append(served, item.Message.Message.Model)
			costs = append(costs, item.Message.Message.Cost)
		}
	}
	assert.Equal(t, []string{"anthropic/" + fallback, "anthropic/" + fallback}, served, "the marker and sticky routing both name the serving model")
	// Each scripted attempt reports 10 input and 7 output tokens. The handoff
	// turn bills the declined Opus attempt and the Sonnet one; the sticky turn
	// bills Sonnet only.
	require.Len(t, costs, 2)
	assert.InDelta(t, (10*10+7*50+10*3+7*15)/1e6, costs[0], 1e-12)
	assert.InDelta(t, (10*3+7*15)/1e6, costs[1], 1e-12)

	loaded.AddMessage(session.UserMessage("u3"))
	reloaded := anthropicRuntime(t, server, primary, opts, openSessionStore(t, dbPath), models, record)
	requireNoErrorEvents(t, drainRunStream(t, reloaded, loaded))

	requests := server.Requests()
	require.Len(t, requests, 3)
	projected := []string{"text", "fallback", "text"}
	assert.Equal(t, [][]string{projected}, assistantBlocks(t, requests[1]), "no declined thinking or tool_use")
	assert.Equal(t, [][]string{projected, {"text"}}, assistantBlocks(t, requests[2]), "the same projection after a reload")
	raw, _ := json.Marshal(requests[2].Body)
	assert.NotContains(t, string(raw), "sig-declined")
	assert.Zero(t, calls.Load())
}
