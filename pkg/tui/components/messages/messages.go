package messages

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/glamour/v2"
	"github.com/charmbracelet/glamour/v2/styles"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/message"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/types"
)

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
	AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd
	AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd
	AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd
	ClearMessages()
	ScrollToBottom() tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd
	AddSystemMessage(content string) tea.Cmd
	PlainTextTranscript() string
}

// renderedItem represents a cached rendered message with position information
type renderedItem struct {
	id     string // Message ID or index as string
	view   string // Cached rendered content
	height int    // Height in lines
	start  int    // Starting line position in complete content
	end    int    // Ending line position in complete content
}

// model implements Model
type model struct {
	renderer    *glamour.TermRenderer
	messages    []types.Message
	views       []layout.Model
	width       int
	height      int
	focused     bool
	app         *app.App
	toolFocused layout.Model

	// Height tracking system fields
	scrollOffset  int                     // Current scroll position in lines
	rendered      string                  // Complete rendered content string
	renderedItems map[string]renderedItem // Cache of rendered items with positions
	totalHeight   int                     // Total height of all content in lines
}

// New creates a new message list component
func New(a *app.App) Model {
	return &model{
		width:         80,
		height:        24,
		app:           a,
		renderedItems: make(map[string]renderedItem),
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
	case tea.WindowSizeMsg:
		cmd := m.SetSize(msg.Width, msg.Height)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

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

	case tea.KeyPressMsg:
		switch msg.String() {
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

		if m.focused && m.toolFocused != nil {
			if updatedModel, cmd := m.toolFocused.Update(msg); cmd != nil {
				m.toolFocused = updatedModel.(layout.Model)
				return m, cmd
			} else {
				m.toolFocused = updatedModel.(layout.Model)
			}
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

	return strings.Join(lines[startLine:endLine], "\n")
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height

	// Ensure minimum width
	if width < 10 {
		width = 10
	}

	// Build a custom style
	customDarkStyle := *styles.DefaultStyles["dark"]
	customDarkStyle.Document.Margin = uintPtr(0)

	// Initialize or update renderer
	if r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStyles(customDarkStyle),
	); err == nil {
		m.renderer = r
	}

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
	m.focused = true
	return nil
}

// Blur removes focus from the component
func (m *model) Blur() tea.Cmd {
	m.focused = false
	return nil
}

// IsFocused returns whether the component is focused
func (m *model) IsFocused() bool {
	return m.focused
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
		// Never cache tool messages - they have dynamic spinners
		return false
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
	id := m.getItemID(index)

	// Only check cache for messages that should be cached
	if m.shouldCacheMessage(index) {
		if cached, exists := m.renderedItems[id]; exists {
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
		id:     id,
		view:   rendered,
		height: height,
	}

	// Only store in cache for messages that should be cached
	if m.shouldCacheMessage(index) {
		m.renderedItems[id] = item
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

	// Render all items and calculate their positions
	var allLines []string
	currentPosition := 0

	for i, view := range m.views {
		item := m.renderItem(i, view)

		// Update position information
		item.start = currentPosition
		if item.height > 0 {
			item.end = currentPosition + item.height - 1
		} else {
			item.end = currentPosition
		}

		// Add content to complete rendered string
		if item.view != "" {
			lines := strings.Split(item.view, "\n")
			allLines = append(allLines, lines...)
			currentPosition += len(lines)
		}

		// Add separator between messages (but not after last message)
		if i < len(m.views)-1 && item.view != "" {
			allLines = append(allLines, "")
			currentPosition += 1
		}

		// Update cache with position information
		m.renderedItems[item.id] = item
	}

	m.rendered = strings.Join(allLines, "\n")
	m.totalHeight = len(allLines)
}

// invalidateItem removes an item from cache, forcing re-render
func (m *model) invalidateItem(index int) {
	// Only invalidate if it was actually cached
	if m.shouldCacheMessage(index) {
		id := m.getItemID(index)
		delete(m.renderedItems, id)
	}
}

// invalidateAllItems clears the entire cache
func (m *model) invalidateAllItems() {
	m.renderedItems = make(map[string]renderedItem)
	m.rendered = ""
	m.totalHeight = 0
}

// getItemID returns a unique ID for a message at the given index
func (m *model) getItemID(index int) string {
	if index >= 0 && index < len(m.messages) {
		// Use a combination of index and message type/content hash for uniqueness
		msg := m.messages[index]
		return fmt.Sprintf("%d-%d-%d", index, int(msg.Type), len(msg.Content))
	}
	return fmt.Sprintf("%d", index)
}

// isAtBottom returns true if the viewport is at the bottom
func (m *model) isAtBottom() bool {
	if len(m.messages) == 0 {
		return true
	}

	totalHeight := lipgloss.Height(m.rendered) - 1
	maxScrollOffset := max(0, totalHeight-m.height)
	return m.scrollOffset >= maxScrollOffset
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
	wasAtBottom := m.isAtBottom()
	m.messages = append(m.messages, *msg)

	view := m.createMessageView(msg)
	m.views = append(m.views, view)

	var cmds []tea.Cmd
	if initCmd := view.Init(); initCmd != nil {
		cmds = append(cmds, initCmd)
	}

	if wasAtBottom {
		cmds = append(cmds, tea.Cmd(func() tea.Msg {
			m.scrollToBottom()
			return nil
		}))
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
			// Update the corresponding view
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
			// Update the corresponding view
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
		wasAtBottom := m.isAtBottom()
		lastMsg.Content += content
		lastMsg.Sender = agentName
		// Update the corresponding view
		view := m.createMessageView(lastMsg)
		m.views[lastIdx] = view
		m.invalidateItem(lastIdx)

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
	} else {
		// Create new assistant message
		msg := types.Message{
			Type:    messageType,
			Content: content,
			Sender:  agentName,
		}
		wasAtBottom := m.isAtBottom()
		m.messages = append(m.messages, msg)

		view := m.createMessageView(&msg)
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
}

// ClearMessages clears all messages
func (m *model) ClearMessages() {
	m.messages = nil
	m.views = nil
	m.scrollOffset = 0
	m.rendered = ""
	m.totalHeight = 0
	m.renderedItems = make(map[string]renderedItem)
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
	view := tool.New(msg, m.app, m.renderer)
	view.SetSize(m.width, 0)
	return view
}

func (m *model) createMessageView(msg *types.Message) layout.Model {
	view := message.New(msg, m.renderer)
	view.SetSize(m.width, 0)
	return view
}

// removeSpinner removes the last message if it's an assistant message with empty content
func (m *model) removeSpinner() {
	if len(m.messages) > 0 {
		lastIdx := len(m.messages) - 1
		lastMessage := m.messages[lastIdx]

		if lastMessage.Type == types.MessageTypeSpinner {
			m.messages = m.messages[:lastIdx]
			if len(m.views) > lastIdx {
				m.views = m.views[:lastIdx]
			}
			m.invalidateItem(lastIdx)
		}
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

func uintPtr(u uint) *uint { return &u }
