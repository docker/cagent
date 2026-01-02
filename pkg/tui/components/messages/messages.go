package messages

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/message"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// StreamCancelledMsg notifies components that the stream has been cancelled
type StreamCancelledMsg struct {
	ShowMessage bool // Whether to show a cancellation message after cleanup
}

// AutoScrollTickMsg triggers auto-scroll during selection
type AutoScrollTickMsg struct {
	Direction int // -1 for up, 1 for down
}

// ToggleHideToolResultsMsg triggers hiding/showing tool results
type ToggleHideToolResultsMsg struct{}

// Model represents a chat message list component
type Model interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	layout.Help
	layout.Positionable

	AddUserMessage(content string) tea.Cmd
	AddErrorMessage(content string) tea.Cmd
	AddAssistantMessage() tea.Cmd
	AddCancelledMessage() tea.Cmd
	AddWelcomeMessage(content string) tea.Cmd
	AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd
	AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd
	AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd

	ScrollToBottom() tea.Cmd
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
	mouseY          int // Screen Y coordinate for autoscroll
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
	messages []*types.Message
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

	sessionState *service.SessionState
	scrollbar    *scrollbar.Model

	xPos, yPos int

	// User scroll state
	userHasScrolled bool // True when user manually scrolls away from bottom

	// Message selection state
	selectedMessageIndex int  // Index of selected message (-1 = no selection)
	focused              bool // Whether the messages component is focused
}

// New creates a new message list component
func New(a *app.App, sessionState *service.SessionState) Model {
	return &model{
		width:                120,
		height:               24,
		app:                  a,
		renderedItems:        make(map[int]renderedItem),
		sessionState:         sessionState,
		scrollbar:            scrollbar.New(),
		selectedMessageIndex: -1,
	}
}

// NewScrollableView creates a simple scrollable view for displaying messages in dialogs
// This is a lightweight version that doesn't require app or session state management
func NewScrollableView(width, height int, sessionState *service.SessionState) Model {
	return &model{
		width:                width,
		height:               height,
		renderedItems:        make(map[int]renderedItem),
		sessionState:         sessionState,
		scrollbar:            scrollbar.New(),
		selectedMessageIndex: -1,
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
func (m *model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case StreamCancelledMsg:
		// Handle stream cancellation internally
		m.removeSpinner()
		m.removePendingToolCallMessages()
		return m, nil
	case tea.WindowSizeMsg:
		cmds = append(cmds, m.SetSize(msg.Width, msg.Height))

	case tea.MouseClickMsg:
		if m.isMouseOnScrollbar(msg.X, msg.Y) {
			return m.handleScrollbarUpdate(msg)
		}

		if msg.Button == tea.MouseLeft {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selection.start(line, col)
			m.selection.mouseY = msg.Y // Store screen Y for autoscroll
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.scrollbar.IsDragging() {
			return m.handleScrollbarUpdate(msg)
		}

		if m.selection.mouseButtonDown && m.selection.active {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selection.update(line, col)
			m.selection.mouseY = msg.Y // Store screen Y for autoscroll

			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if updated, cmd := m.handleScrollbarUpdate(msg); cmd != nil {
			return updated, cmd
		}

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
		const mouseScrollAmount = 2
		buttonStr := msg.Button.String()

		switch buttonStr {
		case "wheelup":
			if m.scrollOffset > 0 {
				m.userHasScrolled = true
				for range mouseScrollAmount {
					m.setScrollOffset(m.scrollOffset - defaultScrollAmount)
				}
			}
		case "wheeldown":
			m.userHasScrolled = true
			for range mouseScrollAmount {
				m.setScrollOffset(m.scrollOffset + defaultScrollAmount)
			}
			// Reset userHasScrolled if we've reached the bottom
			if m.isAtBottom() {
				m.userHasScrolled = false
			}
		}
		// Sync scrollbar with new scroll offset
		m.scrollbar.SetScrollOffset(m.scrollOffset)
		return m, nil

	case AutoScrollTickMsg:
		if m.selection.mouseButtonDown && m.selection.active {
			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case editfile.ToggleDiffViewMsg:
		m.sessionState.ToggleSplitDiffView()
		m.invalidateAllItems()
		return m, nil

	case ToggleHideToolResultsMsg:
		m.sessionState.ToggleHideToolResults()
		m.invalidateAllItems()
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.clearSelection()
			return m, nil
		case "up", "k":
			if m.focused {
				m.selectPreviousMessage()
			} else {
				m.scrollUp()
			}
			return m, nil
		case "down", "j":
			if m.focused {
				m.selectNextMessage()
			} else {
				m.scrollDown()
			}
			return m, nil
		case "c":
			if m.focused && m.selectedMessageIndex >= 0 {
				cmd := m.copySelectedMessageToClipboard()
				return m, cmd
			}
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
		m.views[i] = updatedView
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if len(m.messages) == 0 {
		return ""
	}

	// Store previous total height to detect content growth
	prevTotalHeight := m.totalHeight

	// Ensure all items are rendered and positioned
	m.ensureAllItemsRendered()

	if m.totalHeight == 0 {
		return ""
	}

	// Calculate viewport bounds
	maxScrollOffset := max(0, m.totalHeight-m.height)

	// If content has grown and user hasn't manually scrolled, keep at bottom
	if !m.userHasScrolled && m.totalHeight > prevTotalHeight {
		m.scrollOffset = maxScrollOffset
	} else {
		m.scrollOffset = max(0, min(m.scrollOffset, maxScrollOffset))
	}

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

	m.scrollbar.SetDimensions(m.height, m.totalHeight)
	m.scrollbar.SetScrollOffset(m.scrollOffset)

	// Ensure each line doesn't exceed the width to prevent layout overflow
	for i, line := range visibleLines {
		if ansi.StringWidth(line) > m.width {
			visibleLines[i] = ansi.Truncate(line, m.width, "")
		}
	}

	contentView := strings.Join(visibleLines, "\n")
	scrollbarView := m.scrollbar.View()

	if scrollbarView != "" {
		// Add spacing between content and scrollbar
		spacer := strings.Repeat(" ", m.height)
		spacer = strings.ReplaceAll(spacer, " ", " \n")
		spacer = strings.TrimSuffix(spacer, "\n")
		return lipgloss.JoinHorizontal(lipgloss.Top, contentView, spacer, scrollbarView)
	}

	return contentView
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	// Reserve 1 character for scrollbar
	m.width = width - 2
	m.height = height

	for _, view := range m.views {
		view.SetSize(m.width, 0)
	}

	// Account for AppStyle padding (1 char on left) + 1 char spacing
	m.scrollbar.SetPosition(1+m.xPos+m.width+1, m.yPos)

	// Size changes may affect item rendering, invalidate all items
	m.invalidateAllItems()
	return nil
}

func (m *model) SetPosition(x, y int) tea.Cmd {
	m.xPos = x
	m.yPos = y
	return nil
}

// GetSize returns the current dimensions
func (m *model) GetSize() (width, height int) {
	return m.width, m.height
}

// Focus gives focus to the component
func (m *model) Focus() tea.Cmd {
	m.focused = true
	// Start with last selectable message selected when focusing
	m.selectedMessageIndex = m.findLastSelectableMessage()
	if m.selectedMessageIndex >= 0 {
		m.scrollToSelectedMessage()
	}
	return nil
}

// Blur removes focus from the component
func (m *model) Blur() tea.Cmd {
	m.focused = false
	m.selectedMessageIndex = -1
	return nil
}

// Bindings returns key bindings for the component
func (m *model) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "select prev"),
		),
		key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "select next"),
		),
		key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy message"),
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
		m.userHasScrolled = true
		m.setScrollOffset(max(0, m.scrollOffset-defaultScrollAmount))
	}
}

func (m *model) scrollDown() {
	m.userHasScrolled = true
	m.setScrollOffset(m.scrollOffset + defaultScrollAmount)
	// Reset userHasScrolled if we've reached the bottom
	if m.isAtBottom() {
		m.userHasScrolled = false
	}
}

func (m *model) scrollPageUp() {
	m.userHasScrolled = true
	m.setScrollOffset(max(0, m.scrollOffset-m.height))
}

func (m *model) scrollPageDown() {
	m.userHasScrolled = true
	m.setScrollOffset(m.scrollOffset + m.height)
	// Reset userHasScrolled if we've reached the bottom
	if m.isAtBottom() {
		m.userHasScrolled = false
	}
}

func (m *model) scrollToTop() {
	m.userHasScrolled = true
	m.setScrollOffset(0)
}

func (m *model) scrollToBottom() {
	m.userHasScrolled = false
	m.setScrollOffset(9_999_999) // Will be clamped in View()
}

// isSelectableMessage returns true if the message type can be selected.
// Only assistant messages can be selected.
func (m *model) isSelectableMessage(index int) bool {
	if index < 0 || index >= len(m.messages) {
		return false
	}
	msg := m.messages[index]
	switch msg.Type {
	case types.MessageTypeAssistant,
		types.MessageTypeAssistantReasoning:
		return true
	default:
		return false
	}
}

// findLastSelectableMessage returns the index of the last selectable message, or -1 if none.
func (m *model) findLastSelectableMessage() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

// findPreviousSelectableMessage returns the index of the previous selectable message
// before the given index, or -1 if none.
func (m *model) findPreviousSelectableMessage(fromIndex int) int {
	for i := fromIndex - 1; i >= 0; i-- {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

// findNextSelectableMessage returns the index of the next selectable message
// after the given index, or -1 if none.
func (m *model) findNextSelectableMessage(fromIndex int) int {
	for i := fromIndex + 1; i < len(m.messages); i++ {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

// selectPreviousMessage selects the previous selectable message in the list
func (m *model) selectPreviousMessage() {
	if len(m.messages) == 0 {
		return
	}
	prevIndex := m.findPreviousSelectableMessage(m.selectedMessageIndex)
	if prevIndex >= 0 {
		m.selectedMessageIndex = prevIndex
		m.invalidateAllItems() // Need to re-render to show selection
		m.scrollToSelectedMessage()
	}
}

// selectNextMessage selects the next selectable message in the list
func (m *model) selectNextMessage() {
	if len(m.messages) == 0 {
		return
	}
	nextIndex := m.findNextSelectableMessage(m.selectedMessageIndex)
	if nextIndex >= 0 {
		m.selectedMessageIndex = nextIndex
		m.invalidateAllItems() // Need to re-render to show selection
		m.scrollToSelectedMessage()
	}
}

// scrollToSelectedMessage ensures the selected message is visible
func (m *model) scrollToSelectedMessage() {
	if m.selectedMessageIndex < 0 || m.selectedMessageIndex >= len(m.messages) {
		return
	}

	// Calculate the line range for the selected message
	startLine := 0
	for i := range m.selectedMessageIndex {
		if i < len(m.views) {
			item := m.renderItem(i, m.views[i])
			startLine += item.height
			// Add separator line between messages (except between consecutive tool calls)
			if i < len(m.messages)-1 {
				currentIsToolCall := m.messages[i].Type == types.MessageTypeToolCall
				nextIsToolCall := m.messages[i+1].Type == types.MessageTypeToolCall
				if !currentIsToolCall || !nextIsToolCall {
					startLine++
				}
			}
		}
	}

	var selectedHeight int
	if m.selectedMessageIndex < len(m.views) {
		item := m.renderItem(m.selectedMessageIndex, m.views[m.selectedMessageIndex])
		selectedHeight = item.height
	}
	endLine := startLine + selectedHeight

	// Scroll to make the selected message visible
	if startLine < m.scrollOffset {
		// Message is above viewport, scroll up
		m.userHasScrolled = true
		m.setScrollOffset(startLine)
	} else if endLine > m.scrollOffset+m.height {
		// Message is below viewport, scroll down
		m.userHasScrolled = true
		m.setScrollOffset(endLine - m.height)
	}
}

// copySelectedMessageToClipboard copies the content of the selected message to clipboard
func (m *model) copySelectedMessageToClipboard() tea.Cmd {
	if m.selectedMessageIndex < 0 || m.selectedMessageIndex >= len(m.messages) {
		return nil
	}

	msg := m.messages[m.selectedMessageIndex]
	content := msg.Content

	if content == "" {
		return nil
	}

	return tea.Sequence(
		tea.SetClipboard(content),
		func() tea.Msg {
			_ = clipboard.WriteAll(content)
			return nil
		},
		notification.SuccessCmd("Message copied to clipboard."),
	)
}

// setScrollOffset updates scroll offset and syncs with scrollbar
func (m *model) setScrollOffset(offset int) {
	// Clamp offset to valid range to prevent "dead zones" during overscroll
	maxOffset := max(0, m.totalHeight-m.height)
	m.scrollOffset = max(0, min(offset, maxOffset))
	m.scrollbar.SetScrollOffset(m.scrollOffset)
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
		return msg.ToolStatus == types.ToolStatusCompleted ||
			msg.ToolStatus == types.ToolStatusError ||
			msg.ToolStatus == types.ToolStatusConfirmation
	case types.MessageTypeToolResult:
		return true
	case types.MessageTypeAssistant, types.MessageTypeAssistantReasoning:
		// Only cache assistant messages that have content (completed streaming)
		// Empty assistant messages have spinners and need constant re-rendering
		return strings.Trim(msg.Content, "\r\n\t ") != ""
	case types.MessageTypeUser:
		// Always cache static content
		return true
	default:
		// Unknown types - don't cache to be safe
		return false
	}
}

// renderItem creates a renderedItem for a specific view with selective caching
func (m *model) renderItem(index int, view layout.Model) renderedItem {
	// Check if this message is selected
	isSelected := m.focused && index == m.selectedMessageIndex

	// Update selection state on message views (only message.Model supports selection)
	if msgView, ok := view.(message.Model); ok {
		msgView.SetSelected(isSelected)
	}

	// Don't cache selected items since selection state can change
	if !isSelected && m.shouldCacheMessage(index) {
		if cached, exists := m.renderedItems[index]; exists {
			return cached
		}
	}

	// Render the item (always for dynamic content, or when not cached)
	rendered := view.View()
	height := lipgloss.Height(rendered)
	if rendered == "" {
		height = 0
	}

	item := renderedItem{
		view:   rendered,
		height: height,
	}

	// Only store in cache for messages that should be cached and not selected
	if !isSelected && m.shouldCacheMessage(index) {
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
		if item.view == "" {
			continue
		}

		// Add content to complete rendered string
		viewContent := strings.TrimSuffix(item.view, "\n")
		lines := strings.Split(viewContent, "\n")

		allLines = append(allLines, lines...)

		// Add separator between messages, but not between consecutive tool calls
		if i < len(m.views)-1 {
			currentIsToolCall := m.messages[i].Type == types.MessageTypeToolCall
			nextIsToolCall := m.messages[i+1].Type == types.MessageTypeToolCall
			if !currentIsToolCall || !nextIsToolCall {
				allLines = append(allLines, "")
			}
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

// isAtBottom returns true if the viewport is at the bottom
func (m *model) isAtBottom() bool {
	if len(m.messages) == 0 {
		return true
	}

	maxScrollOffset := max(0, m.totalHeight-m.height)
	return m.scrollOffset >= maxScrollOffset
}

// AddUserMessage adds a user message to the chat
func (m *model) AddUserMessage(content string) tea.Cmd {
	return m.addMessage(types.User(content))
}

func (m *model) AddErrorMessage(content string) tea.Cmd {
	m.removeSpinner()
	return m.addMessage(types.Error(content))
}

func (m *model) AddShellOutputMessage(content string) tea.Cmd {
	return m.addMessage(types.ShellOutput(content))
}

// AddAssistantMessage adds an assistant message to the chat
func (m *model) AddAssistantMessage() tea.Cmd {
	return m.addMessage(types.Spinner())
}

func (m *model) addMessage(msg *types.Message) tea.Cmd {
	m.clearSelection()

	// Only auto-scroll if user hasn't manually scrolled away
	shouldAutoScroll := !m.userHasScrolled

	m.messages = append(m.messages, msg)

	view := m.createMessageView(msg)
	m.sessionState.PreviousMessage = msg
	m.views = append(m.views, view)

	var cmds []tea.Cmd
	if initCmd := view.Init(); initCmd != nil {
		cmds = append(cmds, initCmd)
	}

	if shouldAutoScroll {
		cmds = append(cmds, func() tea.Msg {
			m.scrollToBottom()
			return nil
		})
	}

	return tea.Batch(cmds...)
}

// AddCancelledMessage adds a cancellation indicator to the chat
func (m *model) AddCancelledMessage() tea.Cmd {
	msg := types.Cancelled()
	m.messages = append(m.messages, msg)

	view := m.createMessageView(msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddWelcomeMessage adds a welcome message to the chat
func (m *model) AddWelcomeMessage(content string) tea.Cmd {
	if content == "" || len(m.views) > 0 {
		return nil
	}
	msg := types.Welcome(content)
	m.messages = append(m.messages, msg)

	view := m.createMessageView(msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddOrUpdateToolCall adds a tool call or updates existing one with the given status
func (m *model) AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd {
	// First try to update existing tool by ID
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.ToolCall.ID == toolCall.ID {
			msg.ToolStatus = status
			if toolCall.Function.Arguments != "" {
				msg.ToolCall.Function.Arguments = toolCall.Function.Arguments
			}
			m.invalidateItem(i)
			return nil
		}
	}

	// If not found by ID, remove last empty assistant message
	m.removeSpinner()

	// Create new tool call
	msg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
	m.messages = append(m.messages, msg)

	view := m.createToolCallView(msg)
	m.views = append(m.views, view)

	return view.Init()
}

// AddToolResult adds tool result to the most recent matching tool call
func (m *model) AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd {
	for i := len(m.messages) - 1; i >= 0; i-- {
		toolMessage := m.messages[i]
		if toolMessage.ToolCall.ID == msg.ToolCall.ID {
			toolMessage.Content = strings.ReplaceAll(msg.Response, "\t", "    ")
			toolMessage.ToolStatus = status
			toolMessage.ToolResult = msg.Result
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
	lastMsg := m.messages[lastIdx]

	if lastMsg.Type == messageType && lastMsg.Sender == agentName {
		lastMsg.Content += content
		m.views[lastIdx].(message.Model).SetMessage(lastMsg)
		m.invalidateItem(lastIdx)
		// Content will auto-scroll in View() if user hasn't scrolled
		return nil
	}

	// Creating a new message, use addMessage for proper auto-scroll
	return m.addMessage(types.Agent(messageType, agentName, content))
}

// ScrollToBottom scrolls to the bottom of the chat
// It only scrolls if the user hasn't manually scrolled away from the bottom
func (m *model) ScrollToBottom() tea.Cmd {
	return func() tea.Msg {
		if !m.userHasScrolled {
			m.scrollToBottom()
		}
		return nil
	}
}

func (m *model) createToolCallView(msg *types.Message) layout.Model {
	view := tool.New(msg, m.sessionState)
	view.SetSize(m.width, 0)
	return view
}

func (m *model) createMessageView(msg *types.Message) layout.Model {
	view := message.New(msg, m.sessionState.PreviousMessage)
	view.SetSize(m.width, 0)
	return view
}

// removeSpinner removes the last message if it's a spinner
func (m *model) removeSpinner() {
	if len(m.messages) == 0 {
		return
	}

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

// removePendingToolCallMessages removes any tool call messages that are in pending or running state
func (m *model) removePendingToolCallMessages() {
	var newMessages []*types.Message
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

// mouseToLineCol converts mouse position to line/column in rendered content
func (m *model) mouseToLineCol(x, y int) (line, col int) {
	// Adjust for left padding (1 column from AppStyle)
	adjustedX := max(0, x-1-m.xPos)
	col = adjustedX

	adjustedY := max(0, y-m.yPos)
	line = m.scrollOffset + adjustedY

	return line, col
}

// boxDrawingChars contains Unicode box-drawing characters used by lipgloss borders.
// These need to be stripped when copying text to clipboard.
var boxDrawingChars = map[rune]bool{
	// Thick border characters
	'┃': true, '━': true, '┏': true, '┓': true, '┗': true, '┛': true,
	// Normal border characters
	'│': true, '─': true, '┌': true, '┐': true, '└': true, '┘': true,
	// Double border characters
	'║': true, '═': true, '╔': true, '╗': true, '╚': true, '╝': true,
	// Rounded border characters
	'╭': true, '╮': true, '╯': true, '╰': true,
	// Block border characters
	'█': true, '▀': true, '▄': true,
	// Additional box-drawing characters that might appear
	'┣': true, '┫': true, '┳': true, '┻': true, '╋': true,
	'├': true, '┤': true, '┬': true, '┴': true, '┼': true,
	'╠': true, '╣': true, '╦': true, '╩': true, '╬': true,
}

// stripBorderChars removes box-drawing characters from text.
// This is used when copying selected text to clipboard to avoid
// including visual border decorations in the copied content.
func stripBorderChars(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if !boxDrawingChars[r] {
			result.WriteRune(r)
		}
	}
	return result.String()
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

	var result strings.Builder
	for i := startLine; i <= endLine && i < len(lines); i++ {
		originalLine := lines[i]
		// Strip ANSI codes first to get the displayed text with borders
		plainLine := ansi.Strip(originalLine)
		// Strip border characters to get the actual text content
		line := stripBorderChars(plainLine)
		runes := []rune(line)

		// Calculate how many display columns were removed by stripping border chars
		// This is needed to adjust the mouse column positions
		borderOffset := runewidth.StringWidth(plainLine) - runewidth.StringWidth(line)

		// Adjust column positions by subtracting the border offset
		adjustedStartCol := max(0, startCol-borderOffset)
		adjustedEndCol := max(0, endCol-borderOffset)

		var lineText string
		switch i {
		case startLine:
			if startLine == endLine {
				startIdx := displayWidthToRuneIndex(line, adjustedStartCol)
				endIdx := min(displayWidthToRuneIndex(line, adjustedEndCol), len(runes))
				if startIdx < len(runes) && startIdx < endIdx {
					lineText = strings.TrimSpace(string(runes[startIdx:endIdx]))
				}
				break
			}
			// First line: from startCol to end
			startIdx := displayWidthToRuneIndex(line, adjustedStartCol)
			if startIdx < len(runes) {
				lineText = strings.TrimSpace(string(runes[startIdx:]))
			}
		case endLine:
			// Last line: from start to endCol
			endIdx := min(displayWidthToRuneIndex(line, adjustedEndCol), len(runes))
			lineText = strings.TrimSpace(string(runes[:endIdx]))
		default:
			// Middle lines: entire line
			lineText = strings.TrimSpace(line)
		}

		if lineText != "" {
			result.WriteString(lineText)
		}

		result.WriteString("\n")
	}

	return result.String()
}

func (m *model) copySelectionToClipboard() tea.Cmd {
	if !m.selection.active {
		return nil
	}

	selectedText := strings.TrimSpace(m.extractSelectedText())
	if selectedText == "" {
		return nil
	}

	return tea.Sequence(
		tea.SetClipboard(selectedText),
		func() tea.Msg {
			_ = clipboard.WriteAll(selectedText)
			return nil
		},
		notification.SuccessCmd("Text copied to clipboard."),
	)
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

	getLineWidth := func(line string) int {
		plainLine := ansi.Strip(line)
		trimmedLine := strings.TrimRight(plainLine, " \t")
		return runewidth.StringWidth(trimmedLine)
	}

	for i, line := range lines {
		absoluteLine := viewportStartLine + i

		if absoluteLine < startLine || absoluteLine > endLine {
			highlighted[i] = line
			continue
		}

		lineWidth := getLineWidth(line)
		switch {
		case startLine == endLine && absoluteLine == startLine:
			// Single line selection
			highlighted[i] = m.highlightLine(line, startCol, min(lineWidth, endCol))
		case absoluteLine == startLine:
			// Start of multi-line selection
			highlighted[i] = m.highlightLine(line, startCol, lineWidth)
		case absoluteLine == endLine:
			// End of multi-line selection
			highlighted[i] = m.highlightLine(line, 0, lineWidth)
		default:
			// Middle of multi-line selection
			highlighted[i] = m.highlightLine(line, 0, lineWidth)
		}
	}

	return highlighted
}

func (m *model) highlightLine(line string, startCol, endCol int) string {
	// Get plain text for boundary checks
	plainLine := ansi.Strip(line)
	plainWidth := runewidth.StringWidth(plainLine)

	// Validate and normalize boundaries
	if startCol >= plainWidth {
		return line
	}
	if startCol >= endCol {
		return line
	}
	endCol = min(endCol, plainWidth)

	// Extract the three parts while preserving ANSI codes
	// before: from start to startCol (preserves original styling)
	before := ansi.Cut(line, 0, startCol)

	// selected: from startCol to endCol (strip styling, apply selection style)
	selectedText := ansi.Cut(line, startCol, endCol)
	selectedPlain := ansi.Strip(selectedText)
	selected := styles.SelectionStyle.Render(selectedPlain)

	// after: from endCol to end (preserves original styling)
	after := ansi.Cut(line, endCol, plainWidth)

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

	// Use stored screen Y coordinate to check if mouse is in autoscroll region
	// mouseToLineCol subtracts 2 for header, so viewport-relative Y is mouseY - 2
	viewportY := max(m.selection.mouseY-2, 0)

	if viewportY < scrollThreshold && m.scrollOffset > 0 {
		// Scroll up - mouse is near top of viewport
		direction = -1
		m.scrollUp()
		// Update endLine to reflect new scroll position
		// When scrolling up, content moves up, so mouse points to a line that's 1 less in absolute terms
		m.selection.endLine = max(0, m.selection.endLine-1)
	} else if viewportY >= m.height-scrollThreshold && viewportY < m.height {
		// Scroll down - mouse is near bottom of viewport
		maxScrollOffset := max(0, m.totalHeight-m.height)
		if m.scrollOffset < maxScrollOffset {
			direction = 1
			m.scrollDown()
			// Update endLine to reflect new scroll position
			// When scrolling down, content moves down, so mouse points to a line that's 1 more in absolute terms
			m.selection.endLine++
		}
	}

	if direction == 0 {
		return nil
	}

	return tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg {
		return AutoScrollTickMsg{Direction: direction}
	})
}

func (m *model) isMouseOnScrollbar(x, y int) bool {
	if m.totalHeight <= m.height {
		return false
	}

	// Account for AppStyle padding (1 char on left) + 1 char spacing
	// Scrollbar is positioned at: padding + xPos + width + 1
	scrollbarX := 1 + m.xPos + m.width + 1
	return x == scrollbarX && y >= m.yPos && y < m.yPos+m.height
}

func (m *model) handleScrollbarUpdate(msg tea.Msg) (layout.Model, tea.Cmd) {
	sb, cmd := m.scrollbar.Update(msg)
	m.scrollbar = sb
	m.scrollOffset = m.scrollbar.GetScrollOffset()
	return m, cmd
}
