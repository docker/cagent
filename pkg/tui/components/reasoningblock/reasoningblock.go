package reasoningblock

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

const (
	// previewLines is the number of reasoning lines to show when collapsed.
	previewLines = 3
)

// ToggleMsg is sent when the block should toggle expanded/collapsed state.
type ToggleMsg struct {
	BlockID string
}

// toolEntry holds a tool call message and its view.
type toolEntry struct {
	msg  *types.Message
	view layout.Model
}

// contentItemKind identifies the type of content item.
type contentItemKind int

const (
	contentItemReasoning contentItemKind = iota
	contentItemTool
)

// contentItem represents either reasoning text or a tool call in sequence.
type contentItem struct {
	kind      contentItemKind
	reasoning string // Used when kind is contentItemReasoning
	toolIndex int    // Index into toolEntries when kind is contentItemTool
}

// renderCache holds cached markdown rendering results to avoid re-rendering on every View() call.
// Invalidated when reasoning content or width changes.
type renderCache struct {
	width            int      // width used for rendering
	reasoningVersion int      // version of reasoning content when cached
	lines            []string // all rendered lines (ANSI stripped)
	hasExtra         bool     // whether there's extra content beyond preview
}

// Model represents a collapsible reasoning + tool calls block.
type Model struct {
	id               string
	agentName        string
	contentItems     []contentItem // Ordered sequence of reasoning and tool calls
	toolEntries      []toolEntry   // All tool entries (referenced by contentItems)
	expanded         bool
	width            int
	height           int
	sessionState     *service.SessionState
	reasoningVersion int          // increments when reasoning content changes
	cache            *renderCache // cached rendering results
}

// New creates a new reasoning block.
func New(id, agentName string, sessionState *service.SessionState) *Model {
	return &Model{
		id:           id,
		agentName:    agentName,
		expanded:     false,
		width:        80,
		sessionState: sessionState,
	}
}

// ID returns the block's unique identifier.
func (m *Model) ID() string {
	return m.id
}

// AgentName returns the agent name for this block.
func (m *Model) AgentName() string {
	return m.agentName
}

// SetReasoning sets reasoning content (replaces all content items with a single reasoning item).
func (m *Model) SetReasoning(content string) {
	m.contentItems = []contentItem{{kind: contentItemReasoning, reasoning: content}}
	m.reasoningVersion++
	m.cache = nil // invalidate cache
}

// AppendReasoning appends to the reasoning content.
// Creates a new reasoning item if the last item was a tool, otherwise appends to the last reasoning item.
func (m *Model) AppendReasoning(content string) {
	if content == "" {
		return
	}

	m.reasoningVersion++
	m.cache = nil // invalidate cache

	// If no items yet or last item was a tool, create new reasoning item
	if len(m.contentItems) == 0 {
		m.contentItems = append(m.contentItems, contentItem{kind: contentItemReasoning, reasoning: content})
		return
	}

	lastIdx := len(m.contentItems) - 1
	if m.contentItems[lastIdx].kind == contentItemReasoning {
		// Append to existing reasoning
		m.contentItems[lastIdx].reasoning += content
	} else {
		// Last item was a tool, start new reasoning block
		m.contentItems = append(m.contentItems, contentItem{kind: contentItemReasoning, reasoning: content})
	}
}

// Reasoning returns the full reasoning content (concatenated from all reasoning items).
func (m *Model) Reasoning() string {
	var parts []string
	for _, item := range m.contentItems {
		if item.kind == contentItemReasoning && item.reasoning != "" {
			parts = append(parts, item.reasoning)
		}
	}
	return strings.Join(parts, "\n\n")
}

// AddToolCall adds a tool call to the block.
func (m *Model) AddToolCall(msg *types.Message) tea.Cmd {
	// Check if tool already exists (update case)
	for i, entry := range m.toolEntries {
		if entry.msg.ToolCall.ID == msg.ToolCall.ID {
			m.toolEntries[i].msg = msg
			m.toolEntries[i].view = tool.New(msg, m.sessionState)
			m.toolEntries[i].view.SetSize(m.contentWidth(), 0)
			return m.toolEntries[i].view.Init()
		}
	}

	// New tool call - add to entries and track position in content sequence
	view := tool.New(msg, m.sessionState)
	view.SetSize(m.contentWidth(), 0)
	toolIndex := len(m.toolEntries)
	m.toolEntries = append(m.toolEntries, toolEntry{msg: msg, view: view})
	m.contentItems = append(m.contentItems, contentItem{kind: contentItemTool, toolIndex: toolIndex})
	return view.Init()
}

// UpdateToolCall updates an existing tool call in the block.
func (m *Model) UpdateToolCall(toolCallID string, status types.ToolStatus, args string) {
	for i, entry := range m.toolEntries {
		if entry.msg.ToolCall.ID != toolCallID {
			continue
		}
		entry.msg.ToolStatus = status
		if args != "" {
			entry.msg.ToolCall.Function.Arguments = args
		}
		m.toolEntries[i] = entry
		return
	}
}

// UpdateToolResult updates tool result for a tool call.
func (m *Model) UpdateToolResult(toolCallID, content string, status types.ToolStatus, result *tools.ToolCallResult) tea.Cmd {
	for i, entry := range m.toolEntries {
		if entry.msg.ToolCall.ID != toolCallID {
			continue
		}
		entry.msg.Content = strings.ReplaceAll(content, "\t", "    ")
		entry.msg.ToolStatus = status
		entry.msg.ToolResult = result
		// Recreate view to pick up new state
		view := tool.New(entry.msg, m.sessionState)
		view.SetSize(m.contentWidth(), 0)
		m.toolEntries[i].view = view
		return view.Init()
	}
	return nil
}

// HasToolCall returns true if the block contains the given tool call ID.
func (m *Model) HasToolCall(toolCallID string) bool {
	for _, entry := range m.toolEntries {
		if entry.msg.ToolCall.ID == toolCallID {
			return true
		}
	}
	return false
}

// ToolCount returns the number of tool calls in this block.
func (m *Model) ToolCount() int {
	return len(m.toolEntries)
}

// IsExpanded returns the current expanded state.
func (m *Model) IsExpanded() bool {
	return m.expanded
}

// Toggle switches between expanded and collapsed state.
func (m *Model) Toggle() {
	m.expanded = !m.expanded
}

// SetExpanded sets the expanded state directly.
func (m *Model) SetExpanded(expanded bool) {
	m.expanded = expanded
}

// Init initializes the component.
func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, entry := range m.toolEntries {
		if cmd := entry.view.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	// Forward updates to all tool views (for spinners, etc.)
	var cmds []tea.Cmd
	for i, entry := range m.toolEntries {
		updated, cmd := entry.view.Update(msg)
		m.toolEntries[i].view = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// View renders the block.
func (m *Model) View() string {
	if m.expanded {
		return m.renderExpanded()
	}
	return m.renderCollapsed()
}

// SetSize sets the component dimensions.
func (m *Model) SetSize(width, height int) tea.Cmd {
	if m.width != width {
		m.cache = nil // invalidate cache on width change
	}
	m.width = width
	m.height = height
	contentWidth := m.contentWidth()
	for _, entry := range m.toolEntries {
		entry.view.SetSize(contentWidth, 0)
	}
	return nil
}

// ensureCache computes and caches the rendered reasoning lines if needed.
// Returns the cached result. Safe to call repeatedly; only re-renders when content or width changes.
func (m *Model) ensureCache() *renderCache {
	contentWidth := m.contentWidth()

	// Return existing cache if still valid
	if m.cache != nil && m.cache.width == contentWidth && m.cache.reasoningVersion == m.reasoningVersion {
		return m.cache
	}

	// Compute fresh cache
	reasoning := m.Reasoning()
	var lines []string
	if reasoning != "" {
		rendered, err := markdown.NewRenderer(contentWidth).Render(reasoning)
		if err != nil {
			rendered = reasoning
		}
		clean := strings.TrimRight(ansi.Strip(rendered), "\n\r\t ")
		lines = strings.Split(clean, "\n")
	}

	m.cache = &renderCache{
		width:            contentWidth,
		reasoningVersion: m.reasoningVersion,
		lines:            lines,
		hasExtra:         len(m.toolEntries) > 0 || len(lines) > previewLines,
	}
	return m.cache
}

// GetSize returns the current dimensions.
func (m *Model) GetSize() (int, int) {
	return m.width, m.height
}

// Height calculates the rendered height.
func (m *Model) Height() int {
	return lipgloss.Height(m.View())
}

// contentWidth returns width available for content.
func (m *Model) contentWidth() int {
	return m.width - styles.AssistantMessageStyle.GetHorizontalFrameSize()
}

// renderExpanded renders the full block with all content in order.
func (m *Model) renderExpanded() string {
	var parts []string

	// Header with collapse affordance
	header := m.renderHeader(true)
	parts = append(parts, header)

	// Render content items in order (interleaved reasoning and tools)
	for i, item := range m.contentItems {
		switch item.kind {
		case contentItemReasoning:
			if item.reasoning != "" {
				if i == 0 {
					parts = append(parts, "") // blank line after header for first item
				}
				parts = append(parts, m.renderReasoningChunk(item.reasoning))
			}
		case contentItemTool:
			if item.toolIndex < len(m.toolEntries) {
				// Blank line before first tool in a consecutive group
				if i == 0 || (i > 0 && m.contentItems[i-1].kind == contentItemReasoning) {
					parts = append(parts, "")
				}
				parts = append(parts, m.toolEntries[item.toolIndex].view.View())
				// Blank line after last tool in a consecutive group (next is reasoning or end)
				isLastItem := i == len(m.contentItems)-1
				nextIsReasoning := !isLastItem && m.contentItems[i+1].kind == contentItemReasoning
				if isLastItem || nextIsReasoning {
					parts = append(parts, "")
				}
			}
		}
	}

	return strings.Join(parts, "\n")
}

// renderCollapsed renders the compact preview.
func (m *Model) renderCollapsed() string {
	var parts []string

	// Header with expand affordance
	header := m.renderHeader(false)
	parts = append(parts, header)

	// Last N lines of reasoning
	if m.Reasoning() != "" {
		preview, _ := m.renderReasoningPreviewWithTruncationInfo()
		if preview != "" {
			parts = append(parts, preview)
		}
	}

	// Only show in-progress tool calls (pending/running) in collapsed view
	// Completed tools are hidden to keep the view clean
	inProgressTools := m.getInProgressTools()
	if len(inProgressTools) > 0 {
		parts = append(parts, "") // blank line before tools
		for _, entry := range inProgressTools {
			parts = append(parts, entry.view.View())
		}
	}

	return strings.Join(parts, "\n")
}

// getInProgressTools returns tool entries that are still in progress (pending or running).
func (m *Model) getInProgressTools() []toolEntry {
	var inProgress []toolEntry
	for _, entry := range m.toolEntries {
		if entry.msg.ToolStatus == types.ToolStatusPending ||
			entry.msg.ToolStatus == types.ToolStatusRunning {
			inProgress = append(inProgress, entry)
		}
	}
	return inProgress
}

// hasExtraContent returns true if there's content that would be shown when expanded
// but is hidden when collapsed (truncated reasoning or completed tool calls).
func (m *Model) hasExtraContent() bool {
	return m.ensureCache().hasExtra
}

// renderHeader renders the header line with toggle affordance.
func (m *Model) renderHeader(expanded bool) string {
	badge := styles.ThinkingBadgeStyle.Render("Thinking")

	// Use [+] to expand and [-] to collapse
	var indicator string
	switch {
	case expanded:
		indicator = styles.MutedStyle.Bold(true).Render(" [-]")
	case m.hasExtraContent():
		indicator = styles.MutedStyle.Bold(true).Render(" [+]")
	}

	// Add tool count indicator when collapsed
	var toolInfo string
	if !expanded && len(m.toolEntries) > 0 {
		if len(m.toolEntries) == 1 {
			toolInfo = styles.MutedStyle.Render(" (1 tool)")
		} else {
			toolInfo = styles.MutedStyle.Render(" (" + strconv.Itoa(len(m.toolEntries)) + " tools)")
		}
	}

	return styles.AssistantMessageStyle.Render(badge + indicator + toolInfo)
}

// renderReasoningChunk renders a single reasoning chunk with styling.
func (m *Model) renderReasoningChunk(text string) string {
	contentWidth := m.contentWidth()
	rendered, err := markdown.NewRenderer(contentWidth).Render(text)
	if err != nil {
		rendered = text
	}

	// Strip ANSI and apply muted italic style
	clean := strings.TrimRight(ansi.Strip(rendered), "\n\r\t ")
	styled := styles.MutedStyle.Italic(true).Render(clean)

	return styles.AssistantMessageStyle.Render(styled)
}

// renderReasoningPreviewWithTruncationInfo renders the last N lines of reasoning
// and returns whether the content was truncated.
func (m *Model) renderReasoningPreviewWithTruncationInfo() (string, bool) {
	cache := m.ensureCache()
	if len(cache.lines) == 0 {
		return "", false
	}

	// Filter empty lines for preview
	var lines []string
	for _, line := range cache.lines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	// Take last N lines
	start := 0
	reasoningTruncated := false
	if len(lines) > previewLines {
		start = len(lines) - previewLines
		reasoningTruncated = true
	}
	previewLinesContent := lines[start:]

	// Style each line - dim the first line more if there's content above (truncated)
	var styledLines []string
	for i, line := range previewLinesContent {
		if i == 0 && reasoningTruncated {
			// Extra dim first line to indicate more content above
			styledLines = append(styledLines, styles.MutedStyle.Italic(true).Faint(true).Render(line))
		} else {
			styledLines = append(styledLines, styles.MutedStyle.Italic(true).Render(line))
		}
	}

	preview := strings.Join(styledLines, "\n")
	return styles.AssistantMessageStyle.Render(preview), reasoningTruncated
}

// IsHeaderLine returns true if the given line index is the header (line 0).
func (m *Model) IsHeaderLine(lineIdx int) bool {
	return lineIdx == 0
}

// IsToggleLine returns true if clicking this line should toggle the block.
// Only the header is toggleable.
func (m *Model) IsToggleLine(lineIdx int) bool {
	return m.IsHeaderLine(lineIdx) && (m.expanded || m.hasExtraContent())
}
