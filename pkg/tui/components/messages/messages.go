package messages

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/animation"
	"github.com/docker/cagent/pkg/tui/components/message"
	"github.com/docker/cagent/pkg/tui/components/reasoningblock"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

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
	AddLoadingMessage(description string) tea.Cmd
	ReplaceLoadingWithUser(content string) tea.Cmd
	AddErrorMessage(content string) tea.Cmd
	AddAssistantMessage() tea.Cmd
	AddCancelledMessage() tea.Cmd
	AddWelcomeMessage(content string) tea.Cmd
	AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd
	AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd
	AppendToLastMessage(agentName, content string) tea.Cmd
	AppendReasoning(agentName, content string) tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd
	LoadFromSession(sess *session.Session) tea.Cmd

	ScrollToBottom() tea.Cmd
	AdjustBottomSlack(delta int)
	ScrollByWheel(delta int)
}

// renderedItem represents a cached rendered message with position information
type renderedItem struct {
	view   string // Cached rendered content
	height int    // Height in lines
}

// blockIDCounter generates unique IDs for reasoning blocks.
var blockIDCounter atomic.Uint64

func nextBlockID() string {
	id := blockIDCounter.Add(1)
	return "block-" + strconv.FormatUint(id, 10)
}

// model implements Model
type model struct {
	messages []*types.Message
	views    []layout.Model
	width    int // Full width including scrollbar space
	height   int

	// Height tracking system fields
	scrollOffset  int                  // Current scroll position in lines
	bottomSlack   int                  // Extra blank lines added after content shrinks
	renderedLines []string             // Cached rendered content as lines (avoids split/join per frame)
	renderedItems map[int]renderedItem // Cache of rendered items with positions
	totalHeight   int                  // Total height of all content in lines
	renderDirty   bool                 // True when rendered content needs rebuild

	selection selectionState

	sessionState *service.SessionState
	scrollbar    *scrollbar.Model

	xPos, yPos int

	// User scroll state
	userHasScrolled bool // True when user manually scrolls away from bottom

	// Message selection state
	selectedMessageIndex int  // Index of selected message (-1 = no selection)
	focused              bool // Whether the messages component is focused

	// Debug layout mode - highlights truncated lines with red background
	debugLayout bool
}

// New creates a new message list component
func New(sessionState *service.SessionState) Model {
	return newModel(120, 24, sessionState)
}

// NewScrollableView creates a simple scrollable view for displaying messages in dialogs
// This is a lightweight version that doesn't require app or session state management
func NewScrollableView(width, height int, sessionState *service.SessionState) Model {
	return newModel(width, height, sessionState)
}

func newModel(width, height int, sessionState *service.SessionState) *model {
	return &model{
		width:                width,
		height:               height,
		renderedItems:        make(map[int]renderedItem),
		sessionState:         sessionState,
		scrollbar:            scrollbar.New(),
		selectedMessageIndex: -1,
		debugLayout:          os.Getenv("CAGENT_EXPERIMENTAL_DEBUG_LAYOUT") == "1",
		renderDirty:          true,
	}
}

// Init initializes the component
func (m *model) Init() tea.Cmd {
	var cmds []tea.Cmd
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
	case messages.StreamCancelledMsg:
		m.removeSpinner()
		m.removePendingToolCallMessages()
		return m, nil

	case tea.WindowSizeMsg:
		cmds = append(cmds, m.SetSize(msg.Width, msg.Height))

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case AutoScrollTickMsg:
		if m.selection.mouseButtonDown && m.selection.active {
			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case DebouncedCopyMsg:
		cmd := m.handleDebouncedCopy(msg)
		return m, cmd

	case editfile.ToggleDiffViewMsg:
		m.sessionState.ToggleSplitDiffView()
		m.invalidateAllItems()
		return m, nil

	case ToggleHideToolResultsMsg:
		m.sessionState.ToggleHideToolResults()
		m.invalidateAllItems()
		return m, nil

	case messages.ThemeChangedMsg:
		// Theme changed - invalidate all render caches
		m.invalidateAllItems()
		editfile.InvalidateCaches()
		for i, view := range m.views {
			updatedView, cmd := view.Update(msg)
			m.views[i] = updatedView
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case reasoningblock.BlockMsg:
		return m.forwardToReasoningBlock(msg.GetBlockID(), msg)

	case animation.TickMsg:
		// Invalidate render cache if there's animated content that needs redrawing.
		// This ensures fades, spinners, etc. actually update visually on each tick.
		if m.hasAnimatedContent() {
			m.renderDirty = true
		}
		// Fall through to forward tick to all views

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}

	// Forward updates to all message views
	for i, view := range m.views {
		updatedView, cmd := view.Update(msg)
		m.views[i] = updatedView
		if cmd != nil {
			cmds = append(cmds, cmd)
			// Child state changed (e.g., spinner tick), invalidate render cache
			m.renderDirty = true
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	if m.isMouseOnScrollbar(msg.X, msg.Y) {
		return m.handleScrollbarUpdate(msg)
	}

	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	line, col := m.mouseToLineCol(msg.X, msg.Y)

	// Check for reasoning block header toggle
	if msgIdx, localLine := m.globalLineToMessageLine(line); msgIdx >= 0 {
		if block, ok := m.views[msgIdx].(*reasoningblock.Model); ok {
			if block.IsToggleLine(localLine) {
				block.Toggle()
				m.userHasScrolled = true // Prevent auto-scroll jump
				m.bottomSlack = 0
				m.invalidateItem(msgIdx)
				return m, nil
			}
		}
	}

	clickCount := m.selection.detectClickType(line, col)

	switch clickCount {
	case 3: // Triple-click: select line
		m.selectLineAt(line)
		m.selection.pendingCopyID++ // Cancel any pending double-click copy
		cmd := m.copySelectionToClipboard()
		return m, cmd
	case 2: // Double-click: select word with debounced copy
		m.selectWordAt(line, col)
		cmd := m.scheduleDebouncedCopy()
		return m, cmd
	default: // Single click: start drag selection
		m.selection.start(line, col)
		m.selection.mouseY = msg.Y
		return m, nil
	}
}

// globalLineToMessageLine maps a global line index to (message index, local line within message).
// Returns (-1, -1) if the line doesn't correspond to any message.
func (m *model) globalLineToMessageLine(globalLine int) (msgIdx, localLine int) {
	m.ensureAllItemsRendered()

	currentLine := 0
	for i, view := range m.views {
		item := m.renderItem(i, view)
		if item.height == 0 {
			continue
		}

		endLine := currentLine + item.height
		if globalLine >= currentLine && globalLine < endLine {
			return i, globalLine - currentLine
		}

		currentLine = endLine
		if m.needsSeparator(i) {
			currentLine++ // Account for separator line
		}
	}

	return -1, -1
}

func (m *model) handleMouseMotion(msg tea.MouseMotionMsg) (layout.Model, tea.Cmd) {
	if m.scrollbar.IsDragging() {
		return m.handleScrollbarUpdate(msg)
	}

	if m.selection.mouseButtonDown && m.selection.active {
		line, col := m.mouseToLineCol(msg.X, msg.Y)
		m.selection.update(line, col)
		m.selection.mouseY = msg.Y
		cmd := m.autoScroll()
		return m, cmd
	}
	return m, nil
}

func (m *model) handleMouseRelease(msg tea.MouseReleaseMsg) (layout.Model, tea.Cmd) {
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
}

func (m *model) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	switch msg.Button.String() {
	case "wheelup":
		m.scrollByWheel(-1)
	case "wheeldown":
		m.scrollByWheel(1)
	}
	return m, nil
}

func (m *model) handleKeyPress(msg tea.KeyPressMsg) (layout.Model, tea.Cmd) {
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
	return m, nil
}

func (m *model) View() string {
	if len(m.messages) == 0 {
		return ""
	}

	prevTotalHeight := m.totalHeight
	prevScrollableHeight := m.totalHeight + m.bottomSlack
	m.ensureAllItemsRendered()

	if m.totalHeight == 0 {
		return ""
	}

	if m.userHasScrolled {
		m.bottomSlack = 0
	} else {
		delta := m.totalHeight - prevTotalHeight
		if delta < 0 {
			m.bottomSlack += -delta
		} else if delta > 0 && m.bottomSlack > 0 {
			consume := min(delta, m.bottomSlack)
			m.bottomSlack -= consume
		}
	}

	scrollableHeight := m.totalHeight + m.bottomSlack
	maxScrollOffset := max(0, scrollableHeight-m.height)

	// Auto-scroll when content grows beyond any slack.
	if !m.userHasScrolled && scrollableHeight > prevScrollableHeight {
		m.scrollOffset = maxScrollOffset
	} else {
		m.scrollOffset = max(0, min(m.scrollOffset, maxScrollOffset))
	}

	// Use cached lines directly - O(1) instead of O(totalHeight) split
	totalLines := len(m.renderedLines) + m.bottomSlack
	if totalLines == 0 {
		return ""
	}

	startLine := m.scrollOffset
	endLine := min(startLine+m.height, totalLines)

	if startLine >= endLine {
		return ""
	}

	// Copy only the visible window to avoid mutating cached lines
	// This is O(viewportHeight) instead of O(totalHeight)
	visibleLines := make([]string, endLine-startLine)
	for i := startLine; i < endLine; i++ {
		if i < len(m.renderedLines) {
			visibleLines[i-startLine] = m.renderedLines[i]
		}
		// Lines beyond renderedLines are bottom slack (empty strings), already zero-valued
	}

	if m.selection.active {
		visibleLines = m.applySelectionHighlight(visibleLines, startLine)
	}

	m.scrollbar.SetDimensions(m.height, m.totalHeight)
	m.scrollbar.SetScrollOffset(m.scrollOffset)

	// Truncate lines that exceed content width to prevent scrollbar from wrapping
	// When debug layout is enabled, lines that need truncation are displayed with red background
	contentWidth := m.contentWidth()
	for i, line := range visibleLines {
		if ansi.StringWidth(line) > contentWidth {
			truncated := ansi.Truncate(line, contentWidth, "")
			if m.debugLayout {
				visibleLines[i] = styles.BaseStyle.Background(styles.Error).Render(ansi.Strip(truncated))
			} else {
				visibleLines[i] = truncated
			}
		}
	}

	scrollbarView := m.scrollbar.View()

	if scrollbarView != "" {
		// Ensure content is exactly m.height lines by padding with empty lines if needed
		for len(visibleLines) < m.height {
			visibleLines = append(visibleLines, "")
		}
		// Truncate if somehow longer (shouldn't happen but safety check)
		if len(visibleLines) > m.height {
			visibleLines = visibleLines[:m.height]
		}
		contentView := strings.Join(visibleLines, "\n")

		// Create spacer with exactly m.height lines
		spacerLines := make([]string, m.height)
		for i := range spacerLines {
			spacerLines[i] = " " // Single space for each line
		}
		spacer := strings.Join(spacerLines, "\n")

		return lipgloss.JoinHorizontal(lipgloss.Top, contentView, spacer, scrollbarView)
	}

	return strings.Join(visibleLines, "\n")
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height

	// Content width reserves space for scrollbar (2 chars: space + scrollbar)
	contentWidth := m.contentWidth()
	for _, view := range m.views {
		view.SetSize(contentWidth, 0)
	}

	m.scrollbar.SetPosition(1+m.xPos+contentWidth+1, m.yPos)
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
		key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "select prev")),
		key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "select next")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy message")),
	}
}

// Help returns the help information
func (m *model) Help() help.KeyMap {
	return core.NewSimpleHelp(m.Bindings())
}

// Scrolling methods
const (
	defaultScrollAmount = 1
	wheelScrollAmount   = 2
)

func (m *model) scrollUp() {
	if m.scrollOffset > 0 {
		m.userHasScrolled = true
		m.bottomSlack = 0
		m.setScrollOffset(max(0, m.scrollOffset-defaultScrollAmount))
	}
}

func (m *model) scrollDown() {
	m.userHasScrolled = true
	m.bottomSlack = 0
	m.setScrollOffset(m.scrollOffset + defaultScrollAmount)
	if m.isAtBottom() {
		m.userHasScrolled = false
	}
}

func (m *model) scrollPageUp() {
	m.userHasScrolled = true
	m.bottomSlack = 0
	m.setScrollOffset(max(0, m.scrollOffset-m.height))
}

func (m *model) scrollPageDown() {
	m.userHasScrolled = true
	m.bottomSlack = 0
	m.setScrollOffset(m.scrollOffset + m.height)
	if m.isAtBottom() {
		m.userHasScrolled = false
	}
}

func (m *model) scrollToTop() {
	m.userHasScrolled = true
	m.bottomSlack = 0
	m.setScrollOffset(0)
}

func (m *model) scrollToBottom() {
	m.userHasScrolled = false
	m.setScrollOffset(9_999_999) // Will be clamped in View()
}

func (m *model) ScrollByWheel(delta int) {
	m.scrollByWheel(delta)
}

func (m *model) scrollByWheel(delta int) {
	if delta == 0 {
		return
	}

	prevOffset := m.scrollOffset
	m.setScrollOffset(m.scrollOffset + (delta * wheelScrollAmount * defaultScrollAmount))
	if m.scrollOffset == prevOffset {
		return
	}

	m.userHasScrolled = true
	m.bottomSlack = 0
	if m.isAtBottom() {
		m.userHasScrolled = false
	}
}

func (m *model) setScrollOffset(offset int) {
	maxOffset := max(0, m.totalScrollableHeight()-m.height)
	m.scrollOffset = max(0, min(offset, maxOffset))
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

func (m *model) isAtBottom() bool {
	if len(m.messages) == 0 {
		return true
	}
	maxScrollOffset := max(0, m.totalScrollableHeight()-m.height)
	return m.scrollOffset >= maxScrollOffset
}

// Message selection methods
func (m *model) isSelectableMessage(index int) bool {
	if index < 0 || index >= len(m.messages) {
		return false
	}
	msgType := m.messages[index].Type
	return msgType == types.MessageTypeAssistant ||
		msgType == types.MessageTypeAssistantReasoningBlock
}

func (m *model) findLastSelectableMessage() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

func (m *model) findPreviousSelectableMessage(fromIndex int) int {
	for i := fromIndex - 1; i >= 0; i-- {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

func (m *model) findNextSelectableMessage(fromIndex int) int {
	for i := fromIndex + 1; i < len(m.messages); i++ {
		if m.isSelectableMessage(i) {
			return i
		}
	}
	return -1
}

func (m *model) selectPreviousMessage() {
	if len(m.messages) == 0 {
		return
	}
	if prevIndex := m.findPreviousSelectableMessage(m.selectedMessageIndex); prevIndex >= 0 {
		m.selectedMessageIndex = prevIndex
		m.invalidateAllItems()
		m.scrollToSelectedMessage()
	}
}

func (m *model) selectNextMessage() {
	if len(m.messages) == 0 {
		return
	}
	if nextIndex := m.findNextSelectableMessage(m.selectedMessageIndex); nextIndex >= 0 {
		m.selectedMessageIndex = nextIndex
		m.invalidateAllItems()
		m.scrollToSelectedMessage()
	}
}

func (m *model) scrollToSelectedMessage() {
	if m.selectedMessageIndex < 0 || m.selectedMessageIndex >= len(m.messages) {
		return
	}

	// Ensure all items are rendered so totalHeight is accurate
	m.ensureAllItemsRendered()

	// Calculate the line range for the selected message
	startLine := 0
	for i := range m.selectedMessageIndex {
		if i < len(m.views) {
			item := m.renderItem(i, m.views[i])
			startLine += item.height
			if m.needsSeparator(i) {
				startLine++
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
		m.userHasScrolled = true
		m.bottomSlack = 0
		m.setScrollOffset(startLine)
	} else if endLine > m.scrollOffset+m.height {
		m.userHasScrolled = true
		m.bottomSlack = 0
		m.setScrollOffset(endLine - m.height)
	}
}

// Caching methods
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
	case types.MessageTypeAssistant:
		return strings.Trim(msg.Content, "\r\n\t ") != ""
	case types.MessageTypeAssistantReasoningBlock:
		// Don't cache reasoning blocks - they can have spinners for in-progress tools
		return false
	case types.MessageTypeUser:
		return true
	default:
		return false
	}
}

func (m *model) renderItem(index int, view layout.Model) renderedItem {
	isSelected := m.focused && index == m.selectedMessageIndex

	if msgView, ok := view.(message.Model); ok {
		msgView.SetSelected(isSelected)
	}

	shouldCache := !isSelected && m.shouldCacheMessage(index)
	if shouldCache {
		if cached, exists := m.renderedItems[index]; exists {
			return cached
		}
	}

	rendered := view.View()
	height := lipgloss.Height(rendered)
	if rendered == "" {
		height = 0
	}

	item := renderedItem{view: rendered, height: height}

	if shouldCache {
		m.renderedItems[index] = item
	}

	return item
}

func (m *model) needsSeparator(index int) bool {
	if index >= len(m.messages)-1 {
		return false
	}
	currentIsToolCall := m.messages[index].Type == types.MessageTypeToolCall
	nextIsToolCall := m.messages[index+1].Type == types.MessageTypeToolCall

	// Always add a separator before transfer_task, even between consecutive tool calls
	if nextIsToolCall && m.messages[index+1].ToolCall.Function.Name == builtin.ToolNameTransferTask {
		return true
	}

	return !currentIsToolCall || !nextIsToolCall
}

func (m *model) ensureAllItemsRendered() {
	if !m.renderDirty && len(m.renderedLines) > 0 {
		return
	}

	if len(m.views) == 0 {
		m.renderedLines = nil
		m.totalHeight = 0
		m.renderDirty = false
		return
	}

	var allLines []string

	for i, view := range m.views {
		item := m.renderItem(i, view)
		if item.view == "" {
			continue
		}

		viewContent := strings.TrimSuffix(item.view, "\n")
		lines := strings.Split(viewContent, "\n")
		allLines = append(allLines, lines...)

		if m.needsSeparator(i) {
			allLines = append(allLines, "")
		}
	}

	// Store lines directly - avoid join/split on every View() call
	m.renderedLines = allLines
	m.totalHeight = len(allLines)
	m.renderDirty = false
}

func (m *model) invalidateItem(index int) {
	if m.shouldCacheMessage(index) {
		delete(m.renderedItems, index)
	}
	m.renderDirty = true
}

func (m *model) invalidateAllItems() {
	m.renderedItems = make(map[int]renderedItem)
	m.renderedLines = nil
	m.totalHeight = 0
	m.renderDirty = true
}

// forwardToReasoningBlock finds the reasoning block with the given ID and forwards the message to it.
func (m *model) forwardToReasoningBlock(blockID string, msg tea.Msg) (layout.Model, tea.Cmd) {
	for i, tuiMsg := range m.messages {
		if tuiMsg.Type == types.MessageTypeAssistantReasoningBlock {
			if block, ok := m.views[i].(*reasoningblock.Model); ok && block.ID() == blockID {
				updatedView, cmd := m.views[i].Update(msg)
				m.views[i] = updatedView
				m.invalidateItem(i)
				return m, cmd
			}
		}
	}
	return m, nil
}

// Message management methods
func (m *model) AddUserMessage(content string) tea.Cmd {
	return m.addMessage(types.User(content))
}

func (m *model) AddLoadingMessage(description string) tea.Cmd {
	return m.addMessage(types.Loading(description))
}

func (m *model) ReplaceLoadingWithUser(content string) tea.Cmd {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == types.MessageTypeLoading {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			if i < len(m.views) {
				m.views = append(m.views[:i], m.views[i+1:]...)
			}
			m.invalidateAllItems()
			break
		}
	}
	return m.addMessage(types.User(content))
}

func (m *model) AddErrorMessage(content string) tea.Cmd {
	m.removeSpinner()
	return m.addMessage(types.Error(content))
}

func (m *model) AddShellOutputMessage(content string) tea.Cmd {
	return m.addMessage(types.ShellOutput(content))
}

func (m *model) AddAssistantMessage() tea.Cmd {
	return m.addMessage(types.Spinner())
}

func (m *model) AddCancelledMessage() tea.Cmd {
	msg := types.Cancelled()
	m.messages = append(m.messages, msg)
	view := m.createMessageView(msg)
	m.views = append(m.views, view)
	m.renderDirty = true
	return view.Init()
}

func (m *model) AddWelcomeMessage(content string) tea.Cmd {
	if content == "" || len(m.views) > 0 {
		return nil
	}
	msg := types.Welcome(content)
	m.messages = append(m.messages, msg)
	view := m.createMessageView(msg)
	m.views = append(m.views, view)
	m.renderDirty = true
	return view.Init()
}

func (m *model) addMessage(msg *types.Message) tea.Cmd {
	m.clearSelection()
	shouldAutoScroll := !m.userHasScrolled

	m.messages = append(m.messages, msg)
	view := m.createMessageView(msg)
	m.sessionState.SetPreviousMessage(msg)
	m.views = append(m.views, view)
	m.renderDirty = true

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

func (m *model) LoadFromSession(sess *session.Session) tea.Cmd {
	appendSessionMessage := func(msg *types.Message, view layout.Model) {
		m.messages = append(m.messages, msg)
		m.views = append(m.views, view)
		m.sessionState.SetPreviousMessage(msg)
	}

	// getOrCreateReasoningBlock returns an existing reasoning block for the agent if the
	// last message is one, otherwise creates a new one. This combines consecutive
	// reasoning/tool messages from the same agent into a single block.
	getOrCreateReasoningBlock := func(agentName string) *reasoningblock.Model {
		if len(m.messages) > 0 {
			lastIdx := len(m.messages) - 1
			lastMsg := m.messages[lastIdx]
			if lastMsg.Type == types.MessageTypeAssistantReasoningBlock && lastMsg.Sender == agentName {
				if block, ok := m.views[lastIdx].(*reasoningblock.Model); ok {
					return block
				}
			}
		}

		// Create new reasoning block
		block := reasoningblock.New(nextBlockID(), agentName, m.sessionState)
		block.SetSize(m.contentWidth(), 0)

		blockMsg := &types.Message{
			Type:   types.MessageTypeAssistantReasoningBlock,
			Sender: agentName,
		}
		appendSessionMessage(blockMsg, block)
		return block
	}

	// addStandaloneToolCall adds a tool call as a standalone message (not in a reasoning block)
	addStandaloneToolCall := func(agentName string, tc tools.ToolCall, toolDef tools.Tool, toolResults map[string]string) {
		toolMsg := types.ToolCallMessage(agentName, tc, toolDef, types.ToolStatusCompleted)
		// Apply tool result if available
		if result, ok := toolResults[tc.ID]; ok {
			toolMsg.Content = strings.ReplaceAll(result, "\t", "    ")
		}
		view := m.createToolCallView(toolMsg)
		appendSessionMessage(toolMsg, view)
	}

	m.messages = nil
	m.views = nil
	m.renderedItems = make(map[int]renderedItem)
	m.renderedLines = nil
	m.scrollOffset = 0
	m.totalHeight = 0
	m.bottomSlack = 0
	m.selectedMessageIndex = -1

	var cmds []tea.Cmd

	// First pass: collect tool results by ToolCallID
	toolResults := make(map[string]string)
	for _, item := range sess.Messages {
		if !item.IsMessage() {
			continue
		}
		smsg := item.Message
		if smsg.Message.Role == chat.MessageRoleTool && smsg.Message.ToolCallID != "" {
			toolResults[smsg.Message.ToolCallID] = smsg.Message.Content
		}
	}

	for _, item := range sess.Messages {
		if !item.IsMessage() {
			continue
		}

		smsg := item.Message
		if smsg.Implicit {
			continue
		}

		switch smsg.Message.Role {
		case chat.MessageRoleUser:
			msg := types.User(smsg.Message.Content)
			appendSessionMessage(msg, m.createMessageView(msg))
		case chat.MessageRoleAssistant:
			hasReasoning := smsg.Message.ReasoningContent != ""
			hasContent := smsg.Message.Content != ""
			hasToolCalls := len(smsg.Message.ToolCalls) > 0
			var reasoningBlock *reasoningblock.Model

			// Step 1: Handle reasoning content - only create/extend a reasoning block if there's actual reasoning
			if hasReasoning {
				reasoningBlock = getOrCreateReasoningBlock(smsg.AgentName)
				reasoningBlock.AppendReasoning(smsg.Message.ReasoningContent)
				// Update the message content for copying
				lastIdx := len(m.messages) - 1
				if m.messages[lastIdx].Content != "" {
					m.messages[lastIdx].Content += "\n\n"
				}
				m.messages[lastIdx].Content += smsg.Message.ReasoningContent
			}

			// Step 2: Handle assistant content - this breaks the reasoning block chain
			if hasContent {
				msg := types.Agent(types.MessageTypeAssistant, smsg.AgentName, smsg.Message.Content)
				appendSessionMessage(msg, m.createMessageView(msg))
			}

			// Step 3: Handle tool calls
			// Tool calls go into the reasoning block ONLY if there was reasoning content AND no regular content
			if hasToolCalls {
				attachToReasoning := reasoningBlock != nil && !hasContent
				for i, tc := range smsg.Message.ToolCalls {
					var toolDef tools.Tool
					if i < len(smsg.Message.ToolDefinitions) {
						toolDef = smsg.Message.ToolDefinitions[i]
					}

					if attachToReasoning {
						toolMsg := types.ToolCallMessage(smsg.AgentName, tc, toolDef, types.ToolStatusCompleted)
						reasoningBlock.AddToolCall(toolMsg)
						if result, ok := toolResults[tc.ID]; ok {
							reasoningBlock.UpdateToolResult(tc.ID, result, types.ToolStatusCompleted, nil)
						}
						continue
					}

					addStandaloneToolCall(smsg.AgentName, tc, toolDef, toolResults)
				}
			}
		case chat.MessageRoleTool:
			continue
		}
	}

	for _, view := range m.views {
		cmds = append(cmds, view.Init())
	}

	cmds = append(cmds, m.ScrollToBottom())
	return tea.Batch(cmds...)
}

func (m *model) AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd {
	// First check if this tool call exists in an active reasoning block
	if block, blockIdx := m.getActiveReasoningBlock(agentName); block != nil {
		if block.HasToolCall(toolCall.ID) {
			block.UpdateToolCall(toolCall.ID, status, toolCall.Function.Arguments)
			m.invalidateItem(blockIdx)
			return nil
		}
	}

	// Then try to update existing standalone tool by ID
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.Type == types.MessageTypeToolCall && msg.ToolCall.ID == toolCall.ID {
			msg.ToolStatus = status
			if toolCall.Function.Arguments != "" {
				msg.ToolCall.Function.Arguments = toolCall.Function.Arguments
			}
			m.invalidateItem(i)
			return nil
		}
	}

	m.removeSpinner()

	// If there's an active reasoning block, add the tool call to it
	if block, blockIdx := m.getActiveReasoningBlock(agentName); block != nil {
		msg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
		cmd := block.AddToolCall(msg)
		m.invalidateItem(blockIdx)
		return cmd
	}

	// Otherwise create a standalone tool call message
	msg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
	m.messages = append(m.messages, msg)
	view := m.createToolCallView(msg)
	m.views = append(m.views, view)
	m.renderDirty = true

	return view.Init()
}

func (m *model) AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd {
	// First check reasoning blocks for the tool call
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == types.MessageTypeAssistantReasoningBlock {
			if block, ok := m.views[i].(*reasoningblock.Model); ok {
				if block.HasToolCall(msg.ToolCall.ID) {
					cmd := block.UpdateToolResult(msg.ToolCall.ID, msg.Response, status, msg.Result)
					m.invalidateItem(i)
					return cmd
				}
			}
		}
	}

	// Then check standalone tool call messages
	for i := len(m.messages) - 1; i >= 0; i-- {
		toolMessage := m.messages[i]
		if toolMessage.Type == types.MessageTypeToolCall && toolMessage.ToolCall.ID == msg.ToolCall.ID {
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

func (m *model) AppendToLastMessage(agentName, content string) tea.Cmd {
	m.removeSpinner()

	if len(m.messages) == 0 {
		return nil
	}

	lastIdx := len(m.messages) - 1
	lastMsg := m.messages[lastIdx]

	// Append to existing assistant message from same agent
	if lastMsg.Type == types.MessageTypeAssistant && lastMsg.Sender == agentName {
		lastMsg.Content += content
		m.views[lastIdx].(message.Model).SetMessage(lastMsg)
		m.invalidateItem(lastIdx)
		return nil
	}

	return m.addMessage(types.Agent(types.MessageTypeAssistant, agentName, content))
}

func (m *model) AppendReasoning(agentName, content string) tea.Cmd {
	m.removeSpinner()

	if len(m.messages) == 0 {
		return m.addReasoningBlock(agentName, content)
	}

	lastIdx := len(m.messages) - 1
	lastMsg := m.messages[lastIdx]

	// Append to existing reasoning block for this agent
	if lastMsg.Type == types.MessageTypeAssistantReasoningBlock && lastMsg.Sender == agentName {
		if block, ok := m.views[lastIdx].(*reasoningblock.Model); ok {
			block.AppendReasoning(content)
			lastMsg.Content += content // Keep content in sync for copying
			m.invalidateItem(lastIdx)
			return nil
		}
	}

	// Create a new reasoning block
	return m.addReasoningBlock(agentName, content)
}

// addReasoningBlock creates a new reasoning block message.
func (m *model) addReasoningBlock(agentName, content string) tea.Cmd {
	m.clearSelection()
	shouldAutoScroll := !m.userHasScrolled

	msg := &types.Message{
		Type:    types.MessageTypeAssistantReasoningBlock,
		Sender:  agentName,
		Content: content,
	}

	block := reasoningblock.New(nextBlockID(), agentName, m.sessionState)
	block.SetReasoning(content)
	block.SetSize(m.contentWidth(), 0)

	m.messages = append(m.messages, msg)
	m.views = append(m.views, block)
	m.sessionState.SetPreviousMessage(msg)
	m.renderDirty = true

	var cmds []tea.Cmd
	if initCmd := block.Init(); initCmd != nil {
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

// getActiveReasoningBlock returns the active reasoning block for the given agent,
// or nil if the last message is not a reasoning block for that agent.
func (m *model) getActiveReasoningBlock(agentName string) (*reasoningblock.Model, int) {
	if len(m.messages) == 0 {
		return nil, -1
	}

	lastIdx := len(m.messages) - 1
	lastMsg := m.messages[lastIdx]

	if lastMsg.Type == types.MessageTypeAssistantReasoningBlock && lastMsg.Sender == agentName {
		if block, ok := m.views[lastIdx].(*reasoningblock.Model); ok {
			return block, lastIdx
		}
	}

	return nil, -1
}

func (m *model) ScrollToBottom() tea.Cmd {
	return func() tea.Msg {
		if !m.userHasScrolled {
			m.scrollToBottom()
		}
		return nil
	}
}

func (m *model) AdjustBottomSlack(delta int) {
	if delta == 0 {
		return
	}
	m.bottomSlack = max(0, m.bottomSlack+delta)
}

// contentWidth returns the width available for content.
// Always reserves 2 chars for scrollbar (space + bar) to prevent layout shifts.
func (m *model) contentWidth() int {
	return m.width - 2
}

func (m *model) totalScrollableHeight() int {
	return m.totalHeight + m.bottomSlack
}

// Helper methods
func (m *model) createToolCallView(msg *types.Message) layout.Model {
	view := tool.New(msg, m.sessionState)
	view.SetSize(m.contentWidth(), 0)
	return view
}

func (m *model) createMessageView(msg *types.Message) layout.Model {
	view := message.New(msg, m.sessionState.PreviousMessage())
	view.SetSize(m.contentWidth(), 0)
	return view
}

func (m *model) removeSpinner() {
	if len(m.messages) == 0 {
		return
	}

	lastIdx := len(m.messages) - 1
	if m.messages[lastIdx].Type == types.MessageTypeSpinner {
		m.messages = m.messages[:lastIdx]
		if len(m.views) > lastIdx {
			m.views = m.views[:lastIdx]
		}
		m.invalidateAllItems()
	}
}

func (m *model) removePendingToolCallMessages() {
	toolCallMessages := make([]*types.Message, 0, len(m.messages))
	views := make([]layout.Model, 0, len(m.views))

	for i, msg := range m.messages {
		if msg.Type == types.MessageTypeToolCall &&
			(msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning) {
			continue
		}

		toolCallMessages = append(toolCallMessages, msg)
		if i < len(m.views) {
			views = append(views, m.views[i])
		}
	}

	if len(toolCallMessages) != len(m.messages) {
		m.messages = toolCallMessages
		m.views = views
		m.invalidateAllItems()
	}
}

func (m *model) mouseToLineCol(x, y int) (line, col int) {
	adjustedX := max(0, x-1-m.xPos)
	adjustedY := max(0, y-m.yPos)
	return m.scrollOffset + adjustedY, adjustedX
}

func (m *model) isMouseOnScrollbar(x, y int) bool {
	if m.totalHeight <= m.height {
		return false
	}
	// Scrollbar is at: 1 (app padding) + xPos + contentWidth + 1 (spacer)
	scrollbarX := 1 + m.xPos + m.contentWidth() + 1
	return x == scrollbarX && y >= m.yPos && y < m.yPos+m.height
}

func (m *model) handleScrollbarUpdate(msg tea.Msg) (layout.Model, tea.Cmd) {
	sb, cmd := m.scrollbar.Update(msg)
	m.scrollbar = sb
	m.userHasScrolled = true
	m.bottomSlack = 0
	m.scrollOffset = m.scrollbar.GetScrollOffset()
	return m, cmd
}

// hasAnimatedContent returns true if the message list contains content that
// requires tick-driven updates (spinners, fades, etc.). Used to decide whether
// to invalidate the render cache on animation ticks.
func (m *model) hasAnimatedContent() bool {
	for i, msg := range m.messages {
		switch msg.Type {
		case types.MessageTypeSpinner, types.MessageTypeLoading:
			// Spinner/loading messages always need ticks
			return true
		case types.MessageTypeToolCall:
			// Tool calls with pending/running status have spinners
			if msg.ToolStatus == types.ToolStatusPending ||
				msg.ToolStatus == types.ToolStatusRunning {
				return true
			}
		case types.MessageTypeAssistantReasoningBlock:
			// Check if reasoning block needs tick updates
			if i < len(m.views) {
				if block, ok := m.views[i].(*reasoningblock.Model); ok {
					if block.NeedsTick() {
						return true
					}
				}
			}
		}
	}
	return false
}
