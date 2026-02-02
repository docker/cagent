package messages

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/animation"
	"github.com/docker/cagent/pkg/tui/components/reasoningblock"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

func TestViewDoesNotWrapWideLines(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(20, 5, sessionState).(*model)
	m.SetSize(20, 5)

	msg := types.Agent(types.MessageTypeAssistant, "", strings.Repeat("x", 200))
	m.messages = append(m.messages, msg)
	m.views = append(m.views, m.createMessageView(msg))

	out := m.View()
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 20)
	}
}

func TestLoadFromSessionIncludesReasoningContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				Message: chat.Message{
					Role:    chat.MessageRoleUser,
					Content: "Hello",
				},
			}),
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Let me think about this...",
					Content:          "Hello back!",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: user message + reasoning block + assistant content = 3 messages
	require.Len(t, m.messages, 3)
	// User message first
	assert.Equal(t, types.MessageTypeUser, m.messages[0].Type)
	assert.Equal(t, "Hello", m.messages[0].Content)
	// Reasoning block second (contains reasoning content)
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[1].Type)
	assert.Equal(t, "Let me think about this...", m.messages[1].Content)
	assert.Equal(t, "root", m.messages[1].Sender)
	// Verify the view is a reasoning block
	block, ok := m.views[1].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, "Let me think about this...", block.Reasoning())
	// Assistant content third
	assert.Equal(t, types.MessageTypeAssistant, m.messages[2].Type)
	assert.Equal(t, "Hello back!", m.messages[2].Content)
	assert.Equal(t, "root", m.messages[2].Sender)
}

func TestLoadFromSessionReasoningOrderWithToolCalls(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "I should call a tool...",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "test_tool", Description: "A test tool"},
					},
					Content: "Tool result processed.",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: reasoning block (reasoning only) + assistant content + standalone tool call = 3 messages
	// The content breaks the reasoning block chain, so tool calls become standalone
	require.Len(t, m.messages, 3)
	// Reasoning block first (contains reasoning only, no tool calls)
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	assert.Equal(t, "I should call a tool...", m.messages[0].Content)
	// Verify the view is a reasoning block without tool calls
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, "I should call a tool...", block.Reasoning())
	assert.Equal(t, 0, block.ToolCount(), "block should NOT contain tool calls (content broke the chain)")
	// Assistant content second
	assert.Equal(t, types.MessageTypeAssistant, m.messages[1].Type)
	assert.Equal(t, "Tool result processed.", m.messages[1].Content)
	// Tool call is standalone (after content broke the chain)
	assert.Equal(t, types.MessageTypeToolCall, m.messages[2].Type)
	assert.Equal(t, "test_tool", m.messages[2].ToolCall.Function.Name)
}

func TestLoadFromSessionReasoningOnlyNoContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Thinking deeply...",
					Content:          "", // No visible content, only reasoning
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: just the reasoning block (no assistant content)
	require.Len(t, m.messages, 1)
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	assert.Equal(t, "Thinking deeply...", m.messages[0].Content)
	// Verify the view is a reasoning block
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, "Thinking deeply...", block.Reasoning())
}

func TestLoadFromSessionToolCallsOnlyNoReasoning(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "", // No reasoning
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "test_tool", Description: "A test tool"},
					},
					Content: "Done.",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: assistant content + standalone tool call = 2 messages
	// Tool calls without reasoning should NOT go into a reasoning block
	require.Len(t, m.messages, 2)
	// Assistant content first (content is rendered before tool calls)
	assert.Equal(t, types.MessageTypeAssistant, m.messages[0].Type)
	assert.Equal(t, "Done.", m.messages[0].Content)
	// Tool call is standalone (not in a reasoning block)
	assert.Equal(t, types.MessageTypeToolCall, m.messages[1].Type)
	assert.Equal(t, "test_tool", m.messages[1].ToolCall.Function.Name)
}

func TestLoadFromSessionWithToolResults(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			// Assistant message with tool calls
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Let me check this...",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "read_file", Arguments: `{"path": "test.txt"}`}},
						{ID: "call-2", Function: tools.FunctionCall{Name: "list_dir", Arguments: `{"path": "."}`}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "read_file", Description: "Read a file"},
						{Name: "list_dir", Description: "List directory"},
					},
				},
			}),
			// Tool result for call-1
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "File content here",
				},
			}),
			// Tool result for call-2
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-2",
					Content:    "file1.txt\nfile2.txt",
				},
			}),
			// Final assistant response
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:    chat.MessageRoleAssistant,
					Content: "I found the files.",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: reasoning block (reasoning + 2 tool calls with results) + assistant content = 2 messages
	require.Len(t, m.messages, 2)

	// First message should be reasoning block
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, "Let me check this...", block.Reasoning())
	assert.Equal(t, 2, block.ToolCount(), "block should contain 2 tool calls")

	// Expand the block to see tool calls (completed tools are hidden when collapsed)
	block.SetExpanded(true)
	view := block.View()
	assert.Contains(t, view, "read_file", "expanded view should show read_file tool")
	assert.Contains(t, view, "list_dir", "expanded view should show list_dir tool")

	// Second message should be assistant content
	assert.Equal(t, types.MessageTypeAssistant, m.messages[1].Type)
	assert.Equal(t, "I found the files.", m.messages[1].Content)
}

func TestLoadFromSessionCombinesConsecutiveReasoningBlocks(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			// First assistant message with reasoning and tool call
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "First reasoning chunk.",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "tool1", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "tool1", Description: "First tool"},
					},
				},
			}),
			// Tool result for call-1
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "Result 1",
				},
			}),
			// Second assistant message with more reasoning and another tool call (consecutive, no content between)
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Second reasoning chunk.",
					ToolCalls: []tools.ToolCall{
						{ID: "call-2", Function: tools.FunctionCall{Name: "tool2", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "tool2", Description: "Second tool"},
					},
				},
			}),
			// Tool result for call-2
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-2",
					Content:    "Result 2",
				},
			}),
			// Third consecutive reasoning block
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Third reasoning chunk.",
					ToolCalls: []tools.ToolCall{
						{ID: "call-3", Function: tools.FunctionCall{Name: "tool3", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "tool3", Description: "Third tool"},
					},
				},
			}),
			// Tool result for call-3
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-3",
					Content:    "Result 3",
				},
			}),
			// Final assistant response (this breaks the chain)
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:    chat.MessageRoleAssistant,
					Content: "All done!",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Should have: 1 combined reasoning block + 1 assistant content = 2 messages
	require.Len(t, m.messages, 2, "consecutive reasoning blocks should be combined into one")

	// First message should be the combined reasoning block
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")

	// Block should contain all 3 tool calls
	assert.Equal(t, 3, block.ToolCount(), "combined block should contain all 3 tool calls")

	// Reasoning should contain all three chunks
	reasoning := block.Reasoning()
	assert.Contains(t, reasoning, "First reasoning chunk", "should contain first reasoning")
	assert.Contains(t, reasoning, "Second reasoning chunk", "should contain second reasoning")
	assert.Contains(t, reasoning, "Third reasoning chunk", "should contain third reasoning")

	// Expand to verify tools are present
	block.SetExpanded(true)
	view := block.View()
	assert.Contains(t, view, "tool1", "should contain tool1")
	assert.Contains(t, view, "tool2", "should contain tool2")
	assert.Contains(t, view, "tool3", "should contain tool3")

	// Second message should be assistant content
	assert.Equal(t, types.MessageTypeAssistant, m.messages[1].Type)
	assert.Equal(t, "All done!", m.messages[1].Content)
}

func TestLoadFromSessionStandaloneToolCallsWithResults(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			// Assistant message with tool calls only (no reasoning, no content)
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role: chat.MessageRoleAssistant,
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "test_tool", Arguments: `{"arg": "value"}`}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "test_tool", Description: "A test tool"},
					},
				},
			}),
			// Tool result
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "Tool execution result",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: standalone tool call (not in reasoning block)
	require.Len(t, m.messages, 1)

	// Tool call should be standalone with result applied
	assert.Equal(t, types.MessageTypeToolCall, m.messages[0].Type)
	assert.Equal(t, "test_tool", m.messages[0].ToolCall.Function.Name)
	assert.Equal(t, "Tool execution result", m.messages[0].Content, "tool result should be applied to standalone tool call")
	assert.Equal(t, types.ToolStatusCompleted, m.messages[0].ToolStatus)
}

func TestLoadFromSessionToolCallsDuringReasoningNoContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			// Assistant message with reasoning and tool calls but NO content
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Let me think and use a tool...",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "think_tool", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "think_tool", Description: "A thinking tool"},
					},
					// No Content - tool calls should stay in reasoning block
				},
			}),
			// Tool result
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "Thought result",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	// Expect: reasoning block only (tool call inside it)
	require.Len(t, m.messages, 1)

	// Should be a reasoning block with the tool call inside
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, "Let me think and use a tool...", block.Reasoning())
	assert.Equal(t, 1, block.ToolCount(), "tool call should be inside reasoning block when no content breaks the chain")
}

func TestLoadFromSessionReasoningWithContentToolResultsStandalone(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					ReasoningContent: "Need to call a tool...",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "test_tool", Description: "A test tool"},
					},
					Content: "Done.",
				},
			}),
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "Result\tvalue",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	require.Len(t, m.messages, 3)

	// Reasoning block should NOT contain tool calls
	assert.Equal(t, types.MessageTypeAssistantReasoningBlock, m.messages[0].Type)
	block, ok := m.views[0].(*reasoningblock.Model)
	require.True(t, ok, "view should be a reasoning block")
	assert.Equal(t, 0, block.ToolCount(), "tool calls should be standalone after content breaks the chain")

	// Assistant content second
	assert.Equal(t, types.MessageTypeAssistant, m.messages[1].Type)
	assert.Equal(t, "Done.", m.messages[1].Content)

	// Tool call is standalone with result applied
	assert.Equal(t, types.MessageTypeToolCall, m.messages[2].Type)
	assert.Equal(t, "test_tool", m.messages[2].ToolCall.Function.Name)
	assert.Equal(t, "Result    value", m.messages[2].Content)
	assert.Equal(t, types.ToolStatusCompleted, m.messages[2].ToolStatus)
}

func TestLoadFromSessionMultipleStandaloneToolCallsWithContentAndResults(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:    chat.MessageRoleAssistant,
					Content: "Here you go.",
					ToolCalls: []tools.ToolCall{
						{ID: "call-1", Function: tools.FunctionCall{Name: "tool1", Arguments: "{}"}},
						{ID: "call-2", Function: tools.FunctionCall{Name: "tool2", Arguments: "{}"}},
					},
					ToolDefinitions: []tools.Tool{
						{Name: "tool1", Description: "First tool"},
						{Name: "tool2", Description: "Second tool"},
					},
				},
			}),
			// Tool results (order shouldn't matter)
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-2",
					Content:    "Second\tresult",
				},
			}),
			session.NewMessageItem(&session.Message{
				AgentName: "root",
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					ToolCallID: "call-1",
					Content:    "First result",
				},
			}),
		},
	}

	m.LoadFromSession(sess)

	require.Len(t, m.messages, 3)

	// Assistant content first
	assert.Equal(t, types.MessageTypeAssistant, m.messages[0].Type)
	assert.Equal(t, "Here you go.", m.messages[0].Content)

	// Tool calls are standalone and in the original tool call order
	assert.Equal(t, types.MessageTypeToolCall, m.messages[1].Type)
	assert.Equal(t, "tool1", m.messages[1].ToolCall.Function.Name)
	assert.Equal(t, "First result", m.messages[1].Content)
	assert.Equal(t, types.ToolStatusCompleted, m.messages[1].ToolStatus)

	assert.Equal(t, types.MessageTypeToolCall, m.messages[2].Type)
	assert.Equal(t, "tool2", m.messages[2].ToolCall.Function.Name)
	assert.Equal(t, "Second    result", m.messages[2].Content)
	assert.Equal(t, types.ToolStatusCompleted, m.messages[2].ToolStatus)
}

// dynamicView is a stub layout.Model that changes its View output on Update
// and returns a non-nil command (simulating spinner tick behavior).
type dynamicView struct {
	frame int
}

func (d *dynamicView) Init() tea.Cmd { return nil }
func (d *dynamicView) Update(tea.Msg) (layout.Model, tea.Cmd) {
	d.frame++
	// Return a non-nil command to signal state change (like spinner.Tick)
	return d, func() tea.Msg { return nil }
}

func (d *dynamicView) View() string {
	return "frame-" + strconv.Itoa(d.frame)
}
func (d *dynamicView) SetSize(_, _ int) tea.Cmd { return nil }

func TestRenderCacheInvalidatesOnChildUpdate(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	// Insert a dynamic view that changes on each Update
	msg := types.Spinner()
	m.messages = append(m.messages, msg)
	m.views = append(m.views, &dynamicView{frame: 0})
	m.renderDirty = true

	// First render - should show frame-0
	view1 := m.View()
	assert.Contains(t, view1, "frame-0")

	// Update with any message - dynamic view will increment frame and return a cmd
	m.Update(struct{}{})

	// Second render - cache should be invalidated, showing frame-1
	view2 := m.View()
	assert.Contains(t, view2, "frame-1")
	assert.NotEqual(t, view1, view2, "View should change after Update with non-nil child cmd")
}

func TestRenderCacheInvalidatesOnAnimationTickWithAnimatedContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	// Add a running tool call which has a spinner (animated content)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "running_tool", Arguments: `{}`},
	}, tools.Tool{Name: "running_tool", Description: "A running tool"}, types.ToolStatusRunning)
	m.messages = append(m.messages, toolMsg)
	m.views = append(m.views, m.createToolCallView(toolMsg))
	m.renderDirty = true

	// First render
	view1 := m.View()
	require.Contains(t, view1, "running_tool")

	// Clear the dirty flag to simulate cached state
	m.renderDirty = false

	// Send animation tick - should invalidate cache because we have animated content
	m.Update(animation.TickMsg{Frame: 1})

	// Cache should be marked dirty
	assert.True(t, m.renderDirty, "renderDirty should be true after animation tick with animated content")
}

func TestRenderCacheNotInvalidatedOnAnimationTickWithoutAnimatedContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)

	// Add a completed tool call (no spinner - not animated)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "completed_tool", Arguments: `{}`},
	}, tools.Tool{Name: "completed_tool", Description: "A completed tool"}, types.ToolStatusCompleted)
	m.messages = append(m.messages, toolMsg)
	m.views = append(m.views, m.createToolCallView(toolMsg))
	m.renderDirty = true

	// First render
	view1 := m.View()
	require.Contains(t, view1, "completed_tool")

	// Clear the dirty flag to simulate cached state
	m.renderDirty = false

	// Send animation tick - should NOT invalidate cache because no animated content
	m.Update(animation.TickMsg{Frame: 1})

	// Cache should still be clean (not dirty)
	assert.False(t, m.renderDirty, "renderDirty should remain false after animation tick without animated content")
}

func TestHasAnimatedContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(m *model)
		wantAnimated bool
	}{
		{
			name:         "empty model",
			setupFunc:    func(_ *model) {},
			wantAnimated: false,
		},
		{
			name: "spinner message",
			setupFunc: func(m *model) {
				msg := types.Spinner()
				m.messages = append(m.messages, msg)
				m.views = append(m.views, m.createMessageView(msg))
			},
			wantAnimated: true,
		},
		{
			name: "loading message",
			setupFunc: func(m *model) {
				msg := types.Loading("Loading...")
				m.messages = append(m.messages, msg)
				m.views = append(m.views, m.createMessageView(msg))
			},
			wantAnimated: true,
		},
		{
			name: "pending tool call",
			setupFunc: func(m *model) {
				toolMsg := types.ToolCallMessage("root", tools.ToolCall{
					ID:       "call-1",
					Function: tools.FunctionCall{Name: "pending_tool", Arguments: `{}`},
				}, tools.Tool{Name: "pending_tool"}, types.ToolStatusPending)
				m.messages = append(m.messages, toolMsg)
				m.views = append(m.views, m.createToolCallView(toolMsg))
			},
			wantAnimated: true,
		},
		{
			name: "running tool call",
			setupFunc: func(m *model) {
				toolMsg := types.ToolCallMessage("root", tools.ToolCall{
					ID:       "call-1",
					Function: tools.FunctionCall{Name: "running_tool", Arguments: `{}`},
				}, tools.Tool{Name: "running_tool"}, types.ToolStatusRunning)
				m.messages = append(m.messages, toolMsg)
				m.views = append(m.views, m.createToolCallView(toolMsg))
			},
			wantAnimated: true,
		},
		{
			name: "completed tool call",
			setupFunc: func(m *model) {
				toolMsg := types.ToolCallMessage("root", tools.ToolCall{
					ID:       "call-1",
					Function: tools.FunctionCall{Name: "completed_tool", Arguments: `{}`},
				}, tools.Tool{Name: "completed_tool"}, types.ToolStatusCompleted)
				m.messages = append(m.messages, toolMsg)
				m.views = append(m.views, m.createToolCallView(toolMsg))
			},
			wantAnimated: false,
		},
		{
			name: "assistant message",
			setupFunc: func(m *model) {
				msg := types.Agent(types.MessageTypeAssistant, "root", "Hello")
				m.messages = append(m.messages, msg)
				m.views = append(m.views, m.createMessageView(msg))
			},
			wantAnimated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sessionState := &service.SessionState{}
			m := NewScrollableView(80, 24, sessionState).(*model)
			m.SetSize(80, 24)
			tt.setupFunc(m)
			got := m.hasAnimatedContent()
			assert.Equal(t, tt.wantAnimated, got)
		})
	}
}

// BenchmarkMessagesView_RenderWhileScrolling benchmarks View() with scroll offset changes.
// This measures render cost only (no input handling or coalescing).
func BenchmarkMessagesView_RenderWhileScrolling(b *testing.B) {
	// Create a model with many messages to simulate a long conversation
	sessionState := &service.SessionState{}
	m := NewScrollableView(120, 40, sessionState).(*model)
	m.SetSize(120, 40)

	// Add 100 messages to create substantial history
	for range 100 {
		msg := types.Agent(types.MessageTypeAssistant, "root", strings.Repeat("This is a test message with some content. ", 10))
		m.messages = append(m.messages, msg)
		m.views = append(m.views, m.createMessageView(msg))
	}

	// Initial render to populate cache
	m.View()

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate scrolling by varying scroll offset
	for i := range b.N {
		// Vary scroll position to simulate wheel scrolling
		m.scrollOffset = (i % 50) * 2
		m.scrollbar.SetScrollOffset(m.scrollOffset)
		_ = m.View()
	}
}

// BenchmarkMessagesView_LargeHistory benchmarks View() with a very large message history.
func BenchmarkMessagesView_LargeHistory(b *testing.B) {
	sessionState := &service.SessionState{}
	m := NewScrollableView(120, 40, sessionState).(*model)
	m.SetSize(120, 40)

	// Add 500 messages
	for i := range 500 {
		content := "Message " + strconv.Itoa(i) + ": " + strings.Repeat("content ", 20)
		msg := types.Agent(types.MessageTypeAssistant, "root", content)
		m.messages = append(m.messages, msg)
		m.views = append(m.views, m.createMessageView(msg))
	}

	// Initial render to populate cache
	m.View()

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		m.scrollOffset = (i % 100) * 5
		m.scrollbar.SetScrollOffset(m.scrollOffset)
		_ = m.View()
	}
}
