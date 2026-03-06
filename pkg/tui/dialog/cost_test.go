package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

func TestNewCostDialog(t *testing.T) {
	t.Parallel()

	sess := session.New()

	dialog := NewCostDialog(sess)

	require.NotNil(t, dialog)
}

func TestCostDialogView(t *testing.T) {
	t.Parallel()

	sess := session.New()

	// Add some messages with usage info
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hello",
			Model:   "gpt-4o",
			Usage: &chat.Usage{
				InputTokens:  1000,
				OutputTokens: 500,
			},
			Cost: 0.005,
		},
	})

	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "World",
			Model:   "gpt-4o",
			Usage: &chat.Usage{
				InputTokens:       800,
				OutputTokens:      300,
				CachedInputTokens: 200,
			},
			Cost: 0.003,
		},
	})

	dialog := NewCostDialog(sess)
	// Set a large enough window size
	dialog.SetSize(100, 50)
	view := dialog.View()

	// Check that the view contains expected content
	// The title may be split across lines due to narrow width
	assert.Contains(t, view, "Session Cost")
	assert.Contains(t, view, "Total")
	assert.Contains(t, view, "By Model")
	assert.Contains(t, view, "gpt-4o")
}

func TestCostDialogWithToolCalls(t *testing.T) {
	t.Parallel()

	sess := session.New()

	// Add message with tool calls
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Let me help you",
			Model:   "claude-sonnet-4-0",
			ToolCalls: []tools.ToolCall{
				{ID: "call_1", Function: tools.FunctionCall{Name: "shell", Arguments: `{"cmd":"ls"}`}},
			},
			Usage: &chat.Usage{
				InputTokens:  2000,
				OutputTokens: 100,
			},
			Cost: 0.01,
		},
	})

	dialog := NewCostDialog(sess)
	// Set a large enough window size
	dialog.SetSize(100, 50)
	view := dialog.View()

	// Model name may be split across lines
	assert.Contains(t, view, "claude")
	assert.Contains(t, view, "$0.01")
}

func TestCostDialogEmptySession(t *testing.T) {
	t.Parallel()

	sess := session.New()

	dialog := NewCostDialog(sess)
	// Set a large enough window size
	dialog.SetSize(100, 50)
	view := dialog.View()

	// Should still render without errors
	assert.Contains(t, view, "Session Cost")
	assert.Contains(t, view, "Total")
	assert.Contains(t, view, "$0.00") // Zero cost
}

func TestCostDialogWithCompactionCost(t *testing.T) {
	t.Parallel()

	sess := session.New()

	// Add a regular message with usage
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hello",
			Model:   "gpt-4o",
			Usage: &chat.Usage{
				InputTokens:  1000,
				OutputTokens: 500,
			},
			Cost: 0.005,
		},
	})

	// Add a compaction summary item with cost (simulates what session_compaction.go does)
	sess.Messages = append(sess.Messages, session.Item{
		Summary: "This is a session summary after compaction.",
		Cost:    0.003,
	})

	data := (&costDialog{session: sess}).gatherCostData()

	// Total cost should include both the message cost and the compaction cost
	assert.InDelta(t, 0.008, data.total.cost, 0.0001)

	// There should be 2 entries in the per-message breakdown:
	// one for the assistant message and one for compaction
	require.Len(t, data.messages, 2)
	assert.InDelta(t, 0.005, data.messages[0].cost, 0.0001)
	assert.Equal(t, "compaction", data.messages[1].label)
	assert.InDelta(t, 0.003, data.messages[1].cost, 0.0001)
}

func TestCostDialogCompactionCostRendersInView(t *testing.T) {
	t.Parallel()

	sess := session.New()

	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hello",
			Model:   "gpt-4o",
			Usage: &chat.Usage{
				InputTokens:  1000,
				OutputTokens: 500,
			},
			Cost: 0.005,
		},
	})

	sess.Messages = append(sess.Messages, session.Item{
		Summary: "Session summary.",
		Cost:    0.002,
	})

	dialog := NewCostDialog(sess)
	dialog.SetSize(100, 50)
	view := dialog.View()

	assert.Contains(t, view, "compaction")
	assert.Contains(t, view, "$0.0070") // total: 0.005 + 0.002
}

func TestFormatCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cost     float64
		expected string
	}{
		{0.0, "$0.00"},
		{0.00001, "$0.00"},
		{0.0001, "$0.0001"},
		{0.001, "$0.0010"},
		{0.01, "$0.01"},
		{0.1, "$0.10"},
		{1.0, "$1.00"},
		{10.5, "$10.50"},
	}

	for _, tt := range tests {
		result := formatCost(tt.cost)
		assert.Equal(t, tt.expected, result, "formatCost(%f)", tt.cost)
	}
}

func TestFormatTokenCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count    int64
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{10000, "10.0K"},
		{999999, "1000.0K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{10000000, "10.0M"},
	}

	for _, tt := range tests {
		result := formatTokenCount(tt.count)
		assert.Equal(t, tt.expected, result, "formatTokenCount(%d)", tt.count)
	}
}
