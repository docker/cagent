package messages

import (
	"os"
	"strings"

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
	"github.com/docker/cagent/pkg/tui/components/message"
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
	AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd
	LoadFromSession(sess *session.Session) tea.Cmd

	ScrollToBottom() tea.Cmd
}

// renderedItem represents a cached rendered message with position information
type renderedItem struct {
	view   string // Cached rendered content
	height int    // Height in lines
}

// model implements Model
type model struct {
	messages []*types.Message
	views    []layout.Model
	width    int // Full width including scrollbar space
	height   int

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
	case StreamCancelledMsg:
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

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}

	// Forward updates to all message views
	for i, view := range m.views {
		updatedView, cmd := view.Update(msg)
		m.views[i] = updatedView
		cmds = append(cmds, cmd)
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
	const mouseScrollAmount = 2
	switch msg.Button.String() {
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
		if m.isAtBottom() {
			m.userHasScrolled = false
		}
	}
	m.scrollbar.SetScrollOffset(m.scrollOffset)
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

	// NOTE: Rendering all message views can be expensive during fast streaming.
	// This is expected and mitigated upstream via event throttling (see App.throttleEvents)
	// to avoid excessive CPU usage when many partial updates are emitted.
	prevTotalHeight := m.totalHeight
	m.ensureAllItemsRendered()

	if m.totalHeight == 0 {
		return ""
	}

	// Calculate viewport bounds
	maxScrollOffset := max(0, m.totalHeight-m.height)

	// Auto-scroll if content grew and user hasn't manually scrolled
	if !m.userHasScrolled && m.totalHeight > prevTotalHeight {
		m.scrollOffset = maxScrollOffset
	} else {
		m.scrollOffset = max(0, min(m.scrollOffset, maxScrollOffset))
	}

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

	contentView := strings.Join(visibleLines, "\n")
	scrollbarView := m.scrollbar.View()

	if scrollbarView != "" {
		// For proper horizontal layout, all components must have the same height
		contentLines := strings.Split(contentView, "\n")

		// Ensure content is exactly m.height lines by padding with empty lines if needed
		for len(contentLines) < m.height {
			contentLines = append(contentLines, "")
		}
		// Truncate if somehow longer (shouldn't happen but safety check)
		if len(contentLines) > m.height {
			contentLines = contentLines[:m.height]
		}
		paddedContentView := strings.Join(contentLines, "\n")

		// Create spacer with exactly m.height lines
		spacerLines := make([]string, m.height)
		for i := range spacerLines {
			spacerLines[i] = " " // Single space for each line
		}
		spacer := strings.Join(spacerLines, "\n")

		return lipgloss.JoinHorizontal(lipgloss.Top, paddedContentView, spacer, scrollbarView)
	}

	return contentView
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

func (m *model) setScrollOffset(offset int) {
	maxOffset := max(0, m.totalHeight-m.height)
	m.scrollOffset = max(0, min(offset, maxOffset))
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

func (m *model) isAtBottom() bool {
	if len(m.messages) == 0 {
		return true
	}
	maxScrollOffset := max(0, m.totalHeight-m.height)
	return m.scrollOffset >= maxScrollOffset
}

// Message selection methods
func (m *model) isSelectableMessage(index int) bool {
	if index < 0 || index >= len(m.messages) {
		return false
	}
	msgType := m.messages[index].Type
	return msgType == types.MessageTypeAssistant || msgType == types.MessageTypeAssistantReasoning
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
		m.setScrollOffset(startLine)
	} else if endLine > m.scrollOffset+m.height {
		m.userHasScrolled = true
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
	case types.MessageTypeAssistant, types.MessageTypeAssistantReasoning:
		return strings.Trim(msg.Content, "\r\n\t ") != ""
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
	if len(m.views) == 0 {
		m.rendered = ""
		m.totalHeight = 0
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

	m.rendered = strings.Join(allLines, "\n")
	m.totalHeight = len(allLines)
}

func (m *model) invalidateItem(index int) {
	if m.shouldCacheMessage(index) {
		delete(m.renderedItems, index)
	}
}

func (m *model) invalidateAllItems() {
	m.renderedItems = make(map[int]renderedItem)
	m.rendered = ""
	m.totalHeight = 0
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
	return view.Init()
}

func (m *model) addMessage(msg *types.Message) tea.Cmd {
	m.clearSelection()
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

func (m *model) LoadFromSession(sess *session.Session) tea.Cmd {
	appendSessionMessage := func(msg *types.Message, view layout.Model) {
		m.messages = append(m.messages, msg)
		m.views = append(m.views, view)
		m.sessionState.PreviousMessage = msg
	}

	m.messages = nil
	m.views = nil
	m.renderedItems = make(map[int]renderedItem)
	m.rendered = ""
	m.scrollOffset = 0
	m.totalHeight = 0
	m.selectedMessageIndex = -1

	var cmds []tea.Cmd

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
			// Reasoning content comes first (matches live stream order)
			if smsg.Message.ReasoningContent != "" {
				msg := types.Agent(types.MessageTypeAssistantReasoning, smsg.AgentName, smsg.Message.ReasoningContent)
				appendSessionMessage(msg, m.createMessageView(msg))
			}
			for i, tc := range smsg.Message.ToolCalls {
				var toolDef tools.Tool
				if i < len(smsg.Message.ToolDefinitions) {
					toolDef = smsg.Message.ToolDefinitions[i]
				}
				msg := types.ToolCallMessage(smsg.AgentName, tc, toolDef, types.ToolStatusCompleted)
				appendSessionMessage(msg, m.createToolCallView(msg))
			}
			if smsg.Message.Content != "" {
				msg := types.Agent(types.MessageTypeAssistant, smsg.AgentName, smsg.Message.Content)
				appendSessionMessage(msg, m.createMessageView(msg))
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

	m.removeSpinner()

	msg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
	m.messages = append(m.messages, msg)
	view := m.createToolCallView(msg)
	m.views = append(m.views, view)

	return view.Init()
}

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
		return nil
	}

	return m.addMessage(types.Agent(messageType, agentName, content))
}

func (m *model) ScrollToBottom() tea.Cmd {
	return func() tea.Msg {
		if !m.userHasScrolled {
			m.scrollToBottom()
		}
		return nil
	}
}

// contentWidth returns the width available for content.
// Always reserves 2 chars for scrollbar (space + bar) to prevent layout shifts.
func (m *model) contentWidth() int {
	return m.width - 2
}

// Helper methods
func (m *model) createToolCallView(msg *types.Message) layout.Model {
	view := tool.New(msg, m.sessionState)
	view.SetSize(m.contentWidth(), 0)
	return view
}

func (m *model) createMessageView(msg *types.Message) layout.Model {
	view := message.New(msg, m.sessionState.PreviousMessage)
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
		// Only invalidate the removed spinner item instead of all cached items
		delete(m.renderedItems, lastIdx)
	}
}

func (m *model) removePendingToolCallMessages() {
	messages := make([]*types.Message, 0, len(m.messages))
	views := make([]layout.Model, 0, len(m.views))

	for i, msg := range m.messages {
		if msg.Type == types.MessageTypeToolCall &&
			(msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning) {
			continue
		}

		messages = append(messages, msg)
		if i < len(m.views) {
			views = append(views, m.views[i])
		}
	}

	if len(messages) != len(m.messages) {
		m.messages = messages
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
	m.scrollOffset = m.scrollbar.GetScrollOffset()
	return m, cmd
}
