package messages

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/message"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// StreamCancelledMsg notifies components that the stream has been cancelled
type StreamCancelledMsg struct {
	ShowMessage bool // Whether to show a cancellation message after cleanup
}

type EvalSavedMsg struct {
	File string
}

// AutoScrollTickMsg triggers auto-scroll during selection
type AutoScrollTickMsg struct {
	Direction int // -1 for up, 1 for down
}

// Model represents a chat message list component
type Model interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	layout.Help

	AddUserMessage(content string) tea.Cmd
	AddErrorMessage(content string) tea.Cmd
	AddAssistantMessage() tea.Cmd
	AddSeparatorMessage() tea.Cmd
	AddCancelledMessage() tea.Cmd
	AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd
	AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd
	AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd
	ScrollToBottom() tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd
	AddWarningMessage(content string) tea.Cmd
	AddSystemMessage(content string) tea.Cmd
	PlainTextTranscript() string
	IsAtBottom() bool
}

// renderedItem represents a cached rendered message with position information
type renderedItem struct {
	view   string // Cached rendered content
	height int    // Height in lines
}

// selectionState encapsulates all state related to text selection
type selectionState struct {
	active          bool
	startLine       int
	startCol        int
	endLine         int
	endCol          int
	mouseButtonDown bool
}

// start initializes a new selection at the given position
func (s *selectionState) start(line, col int) {
	s.active = true
	s.mouseButtonDown = true
	s.startLine = line
	s.startCol = col
	s.endLine = line
	s.endCol = col
}

// update updates the end position of the selection
func (s *selectionState) update(line, col int) {
	s.endLine = line
	s.endCol = col
}

// end finalizes the selection and stops mouse tracking
func (s *selectionState) end() {
	s.mouseButtonDown = false
}

// model implements Model
type model struct {
	messages []types.Message
	views    []layout.Model
	width    int
	height   int
	app      *app.App

	// Height tracking system fields
	scrollOffset  int                  // Current scroll position in lines
	rendered      string               // Complete rendered content string
	renderedItems map[int]renderedItem // Cache of rendered items with positions
	totalHeight   int                  // Total height of all content in lines

	selection selectionState
}

// New creates a new message list component
func New(a *app.App) Model {
	return &model{
		width:         120,
		height:        24,
		app:           a,
		renderedItems: make(map[int]renderedItem),
	}
}

// Init initializes the component
func (m *model) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Initialize all message views
	for _, view := range m.views {
		if cmd := view.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// Update handles messages and updates the component state
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case StreamCancelledMsg:
		// Handle stream cancellation internally
		m.removeSpinner()
		m.cancelPendingToolCalls()
		return m, nil
	case EvalSavedMsg:
		m.AddSystemMessage(fmt.Sprintf("Eval saved to file %s", msg.File))
		cmd := m.ScrollToBottom()
		return m, cmd
	case tea.WindowSizeMsg:
		cmd := m.SetSize(msg.Width, msg.Height)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selection.start(line, col)
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.selection.mouseButtonDown && m.selection.active {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selection.update(line, col)

			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft && m.selection.mouseButtonDown {
			if m.selection.active {
				line, col := m.mouseToLineCol(msg.X, msg.Y)
				m.selection.update(line, col)
				m.selection.end()
				cmd := m.copySelectionToClipboard()
				return m, cmd
			}
			m.selection.end()
		}
		return m, nil

	case tea.MouseWheelMsg:
		const mouseScrollAmount = 3
		buttonStr := msg.Button.String()

		switch buttonStr {
		case "wheelup":
			for range mouseScrollAmount {
				m.scrollUp()
			}
		case "wheeldown":
			for range mouseScrollAmount {
				m.scrollDown()
			}
		default:
			if msg.Y < 0 {
				for range min(-msg.Y, mouseScrollAmount) {
					m.scrollUp()
				}
			} else if msg.Y > 0 {
				for range min(msg.Y, mouseScrollAmount) {
					m.scrollDown()
				}
			}
		}
		return m, nil

	case AutoScrollTickMsg:
		if m.selection.mouseButtonDown && m.selection.active {
			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.clearSelection()
			return m, nil
		case "up", "k":
			m.scrollUp()
			return m, nil
		case "down", "j":
			m.scrollDown()
			return m, nil
		case "pgup":
			m.scrollPageUp()
			return m, nil
		case "pgdown":
			m.scrollPageDown()
			return m, nil
		case "home":
			m.scrollToTop()
			return m, nil
		case "end":
			m.scrollToBottom()
			return m, nil
		}
	}

	// Forward updates to all message views
	for i, view := range m.views {
		updatedView, cmd := view.Update(msg)
		if updatedView != nil {
			m.views[i] = updatedView.(layout.Model)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if len(m.messages) == 0 {
		return ""
	}

	// Ensure all items are rendered and positioned
	m.ensureAllItemsRendered()

	if m.totalHeight == 0 {
		return ""
	}

	// Calculate viewport bounds
	maxScrollOffset := max(0, m.totalHeight-m.height)
	m.scrollOffset = max(0, min(m.scrollOffset, maxScrollOffset))

	// Extract visible portion from complete rendered content
	lines := strings.Split(m.rendered, "\n")
	if len(lines) == 0 {
		return ""
	}

	startLine := m.scrollOffset
	endLine := min(startLine+m.height, len(lines))

	if startLine >= endLine {
		return ""
	}

	visibleLines := lines[startLine:endLine]

	if m.selection.active {
		visibleLines = m.applySelectionHighlight(visibleLines, startLine)
	}

	return strings.Join(visibleLines, "\n")
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height

	// Update all views with new size
	for _, view := range m.views {
		view.SetSize(width, 0)
	}

	// Size changes may affect item rendering, invalidate all items
	m.invalidateAllItems()
	return nil
}

// GetSize returns the current dimensions
func (m *model) GetSize() (width, height int) {
	return m.width, m.height
}

// Focus gives focus to the component
func (m *model) Focus() tea.Cmd {
	return nil
}

// Blur removes focus from the component
func (m *model) Blur() tea.Cmd {
	return nil
}

// Bindings returns key bindings for the component
func (m *model) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
}

// Help returns the help information
func (m *model) Help() help.KeyMap {
	return core.NewSimpleHelp(m.Bindings())
}

// Simple scrolling methods
const defaultScrollAmount = 1

func (m *model) scrollUp() {
	if m.scrollOffset > 0 {
		m.scrollOffset = max(0, m.scrollOffset-defaultScrollAmount)
	}
}

func (m *model) scrollDown() {
	m.scrollOffset += defaultScrollAmount
}

func (m *model) scrollPageUp() {
	m.scrollOffset = max(0, m.scrollOffset-m.height)
}

func (m *model) scrollPageDown() {
	m.scrollOffset += m.height
}

func (m *model) scrollToTop() {
	m.scrollOffset = 0
}

func (m *model) scrollToBottom() {
	m.scrollOffset = 9_999_999 // Will be clamped in View()
}

// shouldCacheMessage determines if a message should be cached based on its type and content.
// Only static content is cached to improve performance while preserving dynamic animations.
func (m *model) shouldCacheMessage(index int) bool {
	if index < 0 || index >= len(m.messages) {
		return false
	}

	msg := m.messages[index]

	switch msg.Type {
	case types.MessageTypeToolCall:
		return msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError
	case types.MessageTypeToolResult:
		return true
	case types.MessageTypeAssistant:
		// Only cache assistant messages that have content (completed streaming)
		// Empty assistant messages have spinners and need constant re-rendering
		return strings.Trim(msg.Content, "\r\n\t ") != ""
	case types.MessageTypeUser, types.MessageTypeSeparator:
		// Always cache static content
		return true
	default:
		// Unknown types - don't cache to be safe
		return false
	}
}

// renderItem creates a renderedItem for a specific view with selective caching
func (m *model) renderItem(index int, view layout.Model) renderedItem {
	// Only check cache for messages that should be cached
	if m.shouldCacheMessage(index) {
		if cached, exists := m.renderedItems[index]; exists {
			return cached
		}
	}

	// Render the item (always for dynamic content, or when not cached)
	rendered := view.View()
	height := strings.Count(rendered, "\n") + 1
	if rendered == "" {
		height = 0
	}

	item := renderedItem{
		view:   rendered,
		height: height,
	}

	// Only store in cache for messages that should be cached
	if m.shouldCacheMessage(index) {
		m.renderedItems[index] = item
	}

	return item
}

// ensureAllItemsRendered ensures all message items are rendered and positioned
func (m *model) ensureAllItemsRendered() {
	if len(m.views) == 0 {
		m.rendered = ""
		m.totalHeight = 0
		return
	}

	// Render all items and build the full content
	var allLines []string

	for i, view := range m.views {
		item := m.renderItem(i, view)

		// Add content to complete rendered string
		if item.view != "" {
			lines := strings.Split(item.view, "\n")
			allLines = append(allLines, lines...)
		}

		// Add separator between messages (but not after last message)
		if i < len(m.views)-1 && item.view != "" {
			allLines = append(allLines, "")
		}
	}

	m.rendered = strings.Join(allLines, "\n")
	m.totalHeight = len(allLines)
}

// invalidateItem removes an item from cache, forcing re-render
func (m *model) invalidateItem(index int) {
	// Only invalidate if it was actually cached
	if m.shouldCacheMessage(index) {
		delete(m.renderedItems, index)
	}
}

// invalidateAllItems clears the entire cache
func (m *model) invalidateAllItems() {
	m.renderedItems = make(map[int]renderedItem)
	m.rendered = ""
	m.totalHeight = 0
}

// IsAtBottom returns true if the viewport is at the bottom
func (m *model) IsAtBottom() bool {
	if len(m.messages) == 0 {
		return true
	}

	totalHeight := lipgloss.Height(m.rendered) - 1
	maxScrollOffset := max(0, totalHeight-m.height)
	return m.scrollOffset >= maxScrollOffset
}

// isAtBottom is kept as a private method for internal use
func (m *model) isAtBottom() bool {
	return m.IsAtBottom()
}

// AddUserMessage adds a user message to the chat
func (m *model) AddUserMessage(content string) tea.Cmd {
	return m.addMessage(&types.Message{
		Type:    types.MessageTypeUser,
		Content: content,
	})
}

func (m *model) AddErrorMessage(content string) tea.Cmd {
	return m.addMessage(&types.Message{
		Type:    types.MessageTypeError,
		Content: content,
	})
}

func (m *model) AddShellOutputMessage(content string) tea.Cmd {
	return m.addMessage(&types.Message{
		Type:    types.MessageTypeShellOutput,
		Content: content,
	})
}

func (m *model) AddWarningMessage(content string) tea.Cmd {
	// Create a new warning message
	return m.addMessage(&types.Message{
		Type:    types.MessageTypeWarning,
		Content: content,
	})
}

func (m *model) AddSystemMessage(content string) tea.Cmd {
	return m.addMessage(&types.Message{
		Type:    types.MessageTypeSystem,
		Content: content,
	})
}

// AddAssistantMessage adds an assistant message to the chat
func (m *model) AddAssistantMessage() tea.Cmd {
	return m.addMessage(&types.Message{
		Type: types.MessageTypeSpinner,
	})
}

func (m *model) addMessage(msg *types.Message) tea.Cmd {
	m.clearSelection()

	wasAtBottom := m.isAtBottom()

	m.messages = append(m.messages, *msg)

	view := m.createMessageView(msg)
	m.views = append(m.views, view)

	var cmds []tea.Cmd
	if initCmd := view.Init(); initCmd != nil {
		cmds = append(cmds, initCmd)
	}

	if wasAtBottom {
		cmds = append(cmds, func() tea.Msg {
			m.scrollToBottom()
			return nil
		})
	}

	return tea.Batch(cmds...)
}

// AddSeparatorMessage adds a separator message to the chat
func (m *model) AddSeparatorMessage() tea.Cmd {
	m.removeSpinner()
	msg := types.Message{
		Type: types.MessageTypeSeparator,
	}
	m.messages = append(m.messages, msg)

	view := m.createMessageView(&msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddCancelledMessage adds a cancellation indicator to the chat
func (m *model) AddCancelledMessage() tea.Cmd {
	msg := types.Message{
		Type: types.MessageTypeCancelled,
	}
	m.messages = append(m.messages, msg)

	view := m.createMessageView(&msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddOrUpdateToolCall adds a tool call or updates existing one with the given status
func (m *model) AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd {
	// First try to update existing tool by ID
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.ToolCall.ID == toolCall.ID {
			msg.ToolStatus = status
			if toolCall.Function.Arguments != "" {
				msg.ToolCall.Function.Arguments = toolCall.Function.Arguments
			}
			m.invalidateItem(i)

			view := m.createToolCallView(msg)
			m.views[i] = view
			return view.Init()
		}
	}

	// If not found by ID, remove last empty assistant message
	m.removeSpinner()

	// Create new tool call
	msg := types.Message{
		Type:           types.MessageTypeToolCall,
		Sender:         agentName,
		ToolCall:       toolCall,
		ToolDefinition: toolDef,
		ToolStatus:     status,
	}
	m.messages = append(m.messages, msg)

	view := m.createToolCallView(&msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddToolResult adds tool result to the most recent matching tool call
func (m *model) AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd {
	for i := len(m.messages) - 1; i >= 0; i-- {
		toolMessage := &m.messages[i]
		if toolMessage.ToolCall.ID == msg.ToolCall.ID {
			toolMessage.Content = msg.Response
			toolMessage.ToolStatus = status
			m.invalidateItem(i)

			view := m.createToolCallView(toolMessage)
			m.views[i] = view
			return view.Init()
		}
	}
	return nil
}

// AppendToLastMessage appends content to the last message (for streaming)
func (m *model) AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd {
	m.removeSpinner()

	if len(m.messages) == 0 {
		return nil
	}

	lastIdx := len(m.messages) - 1
	lastMsg := &m.messages[lastIdx]

	if lastMsg.Type == messageType {
		lastMsg.Content += content
		m.views[lastIdx].(message.Model).SetMessage(lastMsg)
		m.invalidateItem(lastIdx)
		return nil
	} else {
		// Create new assistant message
		msg := types.Message{
			Type:    messageType,
			Content: content,
			Sender:  agentName,
		}
		m.messages = append(m.messages, msg)

		view := m.createMessageView(&msg)
		m.views = append(m.views, view)

		var cmd tea.Cmd
		if initCmd := view.Init(); initCmd != nil {
			cmd = initCmd
		}
		return cmd
	}
}

// ScrollToBottom scrolls to the bottom of the chat
func (m *model) ScrollToBottom() tea.Cmd {
	return func() tea.Msg {
		m.scrollToBottom()
		return nil
	}
}

// PlainTextTranscript returns the conversation as plain text suitable for copying
func (m *model) PlainTextTranscript() string {
	var builder strings.Builder

	for i := range m.messages {
		msg := m.messages[i]
		switch msg.Type {
		case types.MessageTypeUser:
			writeTranscriptSection(&builder, "User", msg.Content)
		case types.MessageTypeAssistant:
			label := assistantLabel(msg.Sender)
			writeTranscriptSection(&builder, label, msg.Content)
		case types.MessageTypeAssistantReasoning:
			label := assistantLabel(msg.Sender) + " (thinking)"
			writeTranscriptSection(&builder, label, msg.Content)
		case types.MessageTypeShellOutput:
			writeTranscriptSection(&builder, "Shell Output", msg.Content)
		case types.MessageTypeError:
			writeTranscriptSection(&builder, "Error", msg.Content)
		case types.MessageTypeToolCall:
			callLabel := toolCallLabel(msg)
			writeTranscriptSection(&builder, callLabel, formatToolCallContent(msg))
		case types.MessageTypeToolResult:
			resultLabel := toolResultLabel(msg)
			writeTranscriptSection(&builder, resultLabel, msg.Content)
		}
	}

	return strings.TrimSpace(builder.String())
}

func (m *model) createToolCallView(msg *types.Message) layout.Model {
	view := tool.New(msg, m.app, markdown.NewRenderer(m.width))
	view.SetSize(m.width, 0)
	return view
}

func (m *model) createMessageView(msg *types.Message) layout.Model {
	view := message.New(msg)
	view.SetSize(m.width, 0)
	return view
}

// removeSpinner removes the last message if it's a spinner
func (m *model) removeSpinner() {
	if len(m.messages) > 0 {
		lastIdx := len(m.messages) - 1
		lastMessage := m.messages[lastIdx]

		if lastMessage.Type == types.MessageTypeSpinner {
			m.messages = m.messages[:lastIdx]
			if len(m.views) > lastIdx {
				m.views = m.views[:lastIdx]
			}
			// Invalidate all items since we've removed a message
			m.invalidateAllItems()
		}
	}
}

// cancelPendingToolCalls removes any tool calls that are in pending or running state
func (m *model) cancelPendingToolCalls() {
	var newMessages []types.Message
	var newViews []layout.Model

	for i, msg := range m.messages {
		shouldRemove := msg.Type == types.MessageTypeToolCall &&
			(msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning)

		if !shouldRemove {
			newMessages = append(newMessages, msg)
			if i < len(m.views) {
				newViews = append(newViews, m.views[i])
			}
		}
	}

	// Only update if something was actually removed
	if len(newMessages) != len(m.messages) {
		m.messages = newMessages
		m.views = newViews
		// Invalidate all items since we've removed messages
		m.invalidateAllItems()
	}
}

func assistantLabel(sender string) string {
	trimmed := strings.TrimSpace(sender)
	if trimmed == "" || trimmed == "root" {
		return "Assistant"
	}
	return trimmed
}

func writeTranscriptSection(builder *strings.Builder, title, content string) {
	text := strings.TrimSpace(content)
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString("\n\n")
	}
	builder.WriteString(title)
	builder.WriteString(":\n")
	builder.WriteString(text)
}

func toolCallLabel(msg types.Message) string {
	name := strings.TrimSpace(msg.ToolCall.Function.Name)
	if name == "" {
		return "Tool Call"
	}
	return fmt.Sprintf("Tool Call (%s)", name)
}

func formatToolCallContent(msg types.Message) string {
	sender := assistantLabel(msg.Sender)
	name := strings.TrimSpace(msg.ToolCall.Function.Name)
	if name == "" {
		name = "tool"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s invoked %s", sender, name))
	if args := strings.TrimSpace(msg.ToolCall.Function.Arguments); args != "" {
		parts = append(parts, "Arguments:", args)
	}
	return strings.Join(parts, "\n")
}

func toolResultLabel(msg types.Message) string {
	name := strings.TrimSpace(msg.ToolCall.Function.Name)
	if name == "" {
		return "Tool Result"
	}
	return fmt.Sprintf("Tool Result (%s)", name)
}

// mouseToLineCol converts mouse position to line/column in rendered content
func (m *model) mouseToLineCol(x, y int) (line, col int) {
	// Adjust for header (2 lines: text + bottom padding)
	adjustedY := max(0, y-2)
	line = m.scrollOffset + adjustedY

	// Adjust for left padding (1 column from AppStyle)
	adjustedX := max(0, x-1)
	col = adjustedX

	return line, col
}

func (m *model) extractSelectedText() string {
	if !m.selection.active {
		return ""
	}

	lines := strings.Split(m.rendered, "\n")

	// Normalize selection direction
	startLine, startCol := m.selection.startLine, m.selection.startCol
	endLine, endCol := m.selection.endLine, m.selection.endCol

	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	if startLine < 0 || startLine >= len(lines) {
		return ""
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	// Single line selection
	if startLine == endLine {
		if startLine < len(lines) {
			line := ansi.Strip(lines[startLine])
			// Convert display width to rune indices
			startIdx := displayWidthToRuneIndex(line, startCol)
			endIdx := displayWidthToRuneIndex(line, endCol)
			runes := []rune(line)
			if startIdx < len(runes) && startIdx < endIdx {
				if endIdx > len(runes) {
					endIdx = len(runes)
				}
				return string(runes[startIdx:endIdx])
			}
		}
		return ""
	}

	// Multi-line selection
	var result strings.Builder
	for i := startLine; i <= endLine && i < len(lines); i++ {
		line := ansi.Strip(lines[i])
		runes := []rune(line)

		switch i {
		case startLine:
			// First line: from startCol to end
			startIdx := displayWidthToRuneIndex(line, startCol)
			if startIdx < len(runes) {
				result.WriteString(string(runes[startIdx:]))
			}
		case endLine:
			// Last line: from start to endCol
			endIdx := min(displayWidthToRuneIndex(line, endCol), len(runes))
			result.WriteString(string(runes[:endIdx]))
		default:
			// Middle lines: entire line
			result.WriteString(line)
		}

		// Add newline except for last line
		if i < endLine {
			result.WriteString("\n")
		}
	}

	return result.String()
}

func (m *model) copySelectionToClipboard() tea.Cmd {
	if !m.selection.active {
		return nil
	}

	selectedText := m.extractSelectedText()
	if selectedText == "" {
		return nil
	}

	if err := clipboard.WriteAll(selectedText); err != nil {
		return core.CmdHandler(notification.ShowMsg{Text: "Failed to copy: " + err.Error()})
	}

	return core.CmdHandler(notification.ShowMsg{Text: "Text copied to clipboard"})
}

func (m *model) clearSelection() {
	m.selection = selectionState{}
}

func (m *model) applySelectionHighlight(lines []string, viewportStartLine int) []string {
	// Normalize selection bounds
	startLine, startCol := m.selection.startLine, m.selection.startCol
	endLine, endCol := m.selection.endLine, m.selection.endCol

	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	highlighted := make([]string, len(lines))

	for i, line := range lines {
		absoluteLine := viewportStartLine + i

		if absoluteLine < startLine || absoluteLine > endLine {
			highlighted[i] = line
			continue
		}

		switch {
		case startLine == endLine && absoluteLine == startLine:
			// Single line selection
			highlighted[i] = m.highlightLine(line, startCol, endCol)
		case absoluteLine == startLine:
			// Start of multi-line selection
			plainLine := ansi.Strip(line)
			lineWidth := runewidth.StringWidth(plainLine)
			highlighted[i] = m.highlightLine(line, startCol, lineWidth)
		case absoluteLine == endLine:
			// End of multi-line selection
			highlighted[i] = m.highlightLine(line, 0, endCol)
		default:
			// Middle of multi-line selection
			plainLine := ansi.Strip(line)
			lineWidth := runewidth.StringWidth(plainLine)
			highlighted[i] = m.highlightLine(line, 0, lineWidth)
		}
	}

	return highlighted
}

func (m *model) highlightLine(line string, startCol, endCol int) string {
	plainLine := ansi.Strip(line)

	startRuneIdx := displayWidthToRuneIndex(plainLine, startCol)
	endRuneIdx := displayWidthToRuneIndex(plainLine, endCol)

	if startRuneIdx >= len([]rune(plainLine)) {
		return line
	}
	if startRuneIdx >= endRuneIdx {
		return line
	}

	runes := []rune(plainLine)
	before := string(runes[:startRuneIdx])
	selected := styles.SelectionStyle.Render(string(runes[startRuneIdx:endRuneIdx]))
	after := ""
	if endRuneIdx < len(runes) {
		after = string(runes[endRuneIdx:])
	}

	return before + selected + after
}

func displayWidthToRuneIndex(s string, targetWidth int) int {
	if targetWidth <= 0 {
		return 0
	}

	runes := []rune(s)
	currentWidth := 0

	for i, r := range runes {
		if currentWidth >= targetWidth {
			return i
		}
		currentWidth += runewidth.RuneWidth(r)
	}

	return len(runes)
}

func (m *model) autoScroll() tea.Cmd {
	const scrollThreshold = 2
	direction := 0

	if m.selection.endCol < scrollThreshold && m.scrollOffset > 0 {
		// Scroll up
		direction = -1
		m.scrollUp()
		if m.selection.endLine > 0 {
			m.selection.endLine--
		}
	} else if m.selection.endCol >= m.height-scrollThreshold {
		// Scroll down
		maxScrollOffset := max(0, m.totalHeight-m.height)
		if m.scrollOffset < maxScrollOffset {
			direction = 1
			m.scrollDown()
			lines := strings.Split(m.rendered, "\n")
			if m.selection.endLine < len(lines)-1 {
				m.selection.endLine++
			}
		}
	}

	if direction == 0 {
		return nil
	}

	return tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg {
		return AutoScrollTickMsg{Direction: direction}
	})
}
