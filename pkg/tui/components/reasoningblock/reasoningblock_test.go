package reasoningblock

import (
	"strconv"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

func TestReasoningBlockCollapsed(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	reasoning := "Let me think about this problem carefully."
	block.SetReasoning(reasoning)

	// Block starts collapsed
	assert.False(t, block.IsExpanded())

	view := block.View()
	stripped := ansi.Strip(view)

	// Should contain "Thinking" header
	assert.Contains(t, stripped, "Thinking")
	// Short content should NOT show "click to expand" (no extra content to show)
	assert.NotContains(t, stripped, "click to expand")
	// Should contain some reasoning content
	assert.Contains(t, stripped, "think")
}

func TestReasoningBlockCollapsedWithLongContent(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	// Long reasoning that definitely exceeds previewLines (4 lines) after rendering
	// Using markdown list format to ensure line breaks are preserved
	reasoning := `1. First point about the problem
2. Second point to consider
3. Third important aspect
4. Fourth consideration here
5. Fifth point for analysis
6. Final conclusion drawn`
	block.SetReasoning(reasoning)

	// Block starts collapsed
	assert.False(t, block.IsExpanded())

	view := block.View()
	stripped := ansi.Strip(view)

	// Should contain "Thinking" header with expand indicator ([+])
	assert.Contains(t, stripped, "Thinking [+]")
}

func TestReasoningBlockExpanded(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	reasoning := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6"
	block.SetReasoning(reasoning)

	// Expand the block
	block.Toggle()
	assert.True(t, block.IsExpanded())

	view := block.View()
	stripped := ansi.Strip(view)

	// Should contain "Thinking" header with collapse indicator ([-])
	assert.Contains(t, stripped, "Thinking [-]")
	// Should show all lines
	assert.Contains(t, stripped, "Line 1")
	assert.Contains(t, stripped, "Line 6")
}

func TestReasoningBlockWithToolCall(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	block.SetReasoning("Let me think...")

	// Add a running tool call (in-progress tools show in collapsed view)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "read_file", Arguments: `{"path": "/tmp/test.txt"}`},
	}, tools.Tool{Name: "read_file", Description: "Read a file"}, types.ToolStatusRunning)
	block.AddToolCall(toolMsg)

	assert.Equal(t, 1, block.ToolCount())
	assert.True(t, block.HasToolCall("call-1"))
	assert.False(t, block.HasToolCall("call-2"))

	// Collapsed view should show in-progress tool
	view := block.View()
	stripped := ansi.Strip(view)
	assert.Contains(t, stripped, "read_file")
	assert.Contains(t, stripped, "1 tool")
}

func TestReasoningBlockCollapsedShowsToolViews(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	block.SetReasoning("Thinking...")

	// Add a running tool call (in-progress tools show in collapsed view)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "edit_file", Arguments: `{"path": "/tmp/test.txt"}`},
	}, tools.Tool{Name: "edit_file", Description: "Edit a file"}, types.ToolStatusRunning)
	block.AddToolCall(toolMsg)

	// Block is collapsed by default
	assert.False(t, block.IsExpanded())

	view := block.View()
	stripped := ansi.Strip(view)

	// Should show the in-progress tool name
	assert.Contains(t, stripped, "edit_file")
}

func TestReasoningBlockCollapsedHidesCompletedTools(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	block.SetReasoning("Thinking...")

	// Add a completed tool call (should NOT show in collapsed view)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "completed_tool", Arguments: `{}`},
	}, tools.Tool{Name: "completed_tool", Description: "A tool"}, types.ToolStatusCompleted)
	block.AddToolCall(toolMsg)

	// Block is collapsed by default
	assert.False(t, block.IsExpanded())

	view := block.View()
	stripped := ansi.Strip(view)

	// Completed tool should NOT show in collapsed view
	assert.NotContains(t, stripped, "completed_tool")
	// Header should still show tool count
	assert.Contains(t, stripped, "1 tool")

	// When expanded, should show the completed tool
	block.Toggle()
	assert.True(t, block.IsExpanded())
	expandedView := block.View()
	expandedStripped := ansi.Strip(expandedView)
	assert.Contains(t, expandedStripped, "completed_tool")
}

func TestReasoningBlockToggle(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)
	block.SetReasoning("Some reasoning")

	// Initially collapsed
	assert.False(t, block.IsExpanded())

	// Toggle to expanded
	block.Toggle()
	assert.True(t, block.IsExpanded())

	// Toggle back to collapsed
	block.Toggle()
	assert.False(t, block.IsExpanded())
}

func TestReasoningBlockHeaderFooterLineDetection(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)
	// Use markdown list to ensure content exceeds previewLines (4) after rendering
	block.SetReasoning(`1. First point
2. Second point
3. Third point
4. Fourth point
5. Fifth point
6. Sixth point`)

	// When collapsed with extra content, header is toggleable
	assert.True(t, block.IsHeaderLine(0))
	assert.True(t, block.IsToggleLine(0))
	assert.False(t, block.IsToggleLine(1)) // Body line

	// When expanded, header is still toggleable
	block.SetExpanded(true)

	assert.True(t, block.IsHeaderLine(0))
	assert.True(t, block.IsToggleLine(0))
	assert.False(t, block.IsToggleLine(1)) // Body line
}

func TestReasoningBlockMultipleToolCalls(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	block.SetReasoning("Planning steps...")

	// Add multiple running tool calls (in-progress tools show in collapsed view)
	for i := range 3 {
		toolMsg := types.ToolCallMessage("root", tools.ToolCall{
			ID:       "call-" + strconv.Itoa(i),
			Function: tools.FunctionCall{Name: "tool_" + strconv.Itoa(i), Arguments: "{}"},
		}, tools.Tool{Name: "tool_" + strconv.Itoa(i)}, types.ToolStatusRunning)
		block.AddToolCall(toolMsg)
	}

	assert.Equal(t, 3, block.ToolCount())

	// Header should show count
	view := block.View()
	stripped := ansi.Strip(view)
	assert.Contains(t, stripped, "3 tools")

	// Collapsed should show all in-progress tools
	assert.Contains(t, stripped, "tool_0")
	assert.Contains(t, stripped, "tool_1")
	assert.Contains(t, stripped, "tool_2")
}

func TestReasoningBlockAppendReasoning(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	block.SetReasoning("First part")
	assert.Equal(t, "First part", block.Reasoning())

	block.AppendReasoning(" second part")
	assert.Equal(t, "First part second part", block.Reasoning())
}

func TestReasoningBlockEmptyReasoning(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	// Add running tool call without reasoning (in-progress tools show)
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"},
	}, tools.Tool{Name: "test_tool"}, types.ToolStatusRunning)
	block.AddToolCall(toolMsg)

	view := block.View()
	stripped := ansi.Strip(view)

	// Should still render header and in-progress tool
	assert.Contains(t, stripped, "Thinking")
	assert.Contains(t, stripped, "test_tool")
}

func TestReasoningBlockUpdateToolCall(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	// Add a pending tool call
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"},
	}, tools.Tool{Name: "test_tool"}, types.ToolStatusPending)
	block.AddToolCall(toolMsg)

	// Update to completed
	block.UpdateToolCall("call-1", types.ToolStatusCompleted, `{"result": "done"}`)

	// Verify update
	assert.True(t, block.HasToolCall("call-1"))
}

func TestReasoningBlockUpdateToolResult(t *testing.T) {
	t.Parallel()

	sessionState := &service.SessionState{}
	block := New("test-1", "root", sessionState)
	block.SetSize(80, 24)

	// Add a running tool call
	toolMsg := types.ToolCallMessage("root", tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"},
	}, tools.Tool{Name: "test_tool"}, types.ToolStatusRunning)
	block.AddToolCall(toolMsg)

	// Update with result
	result := &tools.ToolCallResult{Output: "Success!"}
	block.UpdateToolResult("call-1", "Success!", types.ToolStatusCompleted, result)

	// Verify the tool is still tracked
	assert.True(t, block.HasToolCall("call-1"))
}
