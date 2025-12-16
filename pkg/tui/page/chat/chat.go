package chat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	msgtypes "github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// FocusedPanel represents which panel is currently focused
type FocusedPanel string

const (
	PanelChat   FocusedPanel = "chat"
	PanelEditor FocusedPanel = "editor"

	sidebarWidth = 40
	// Hide sidebar if window width is less than this
	minWindowWidth = 120
	// Width of the draggable center portion of the resize handle
	resizeHandleWidth = 8
)

// EditorHeightChangedMsg is emitted when the editor height changes (e.g., during resize)
type EditorHeightChangedMsg struct {
	Height int
}

// Page represents the main chat page
type Page interface {
	layout.Model
	layout.Sizeable
	layout.Help
	CompactSession() tea.Cmd
	Cleanup()
	// GetInputHeight returns the current height of the editor/input area (including padding)
	GetInputHeight() int
}

// chatPage implements Page
type chatPage struct {
	width, height int

	// Components
	sidebar  sidebar.Model
	messages messages.Model
	editor   editor.Editor

	sessionState *service.SessionState

	// State
	focusedPanel FocusedPanel

	msgCancel       context.CancelFunc
	streamCancelled bool

	// Key map
	keyMap KeyMap

	app *app.App

	history *history.History

	// Cached layout dimensions
	chatHeight  int
	inputHeight int

	// keyboardEnhancementsSupported tracks whether the terminal supports keyboard enhancements
	keyboardEnhancementsSupported bool

	// Resizable editor state
	isDragging       bool
	isHoveringHandle bool
	editorLines      int
}

// KeyMap defines key bindings for the chat page
type KeyMap struct {
	Tab            key.Binding
	Cancel         key.Binding
	ShiftNewline   key.Binding
	CtrlJ          key.Binding
	ExternalEditor key.Binding
}

// defaultKeyMap returns the default key bindings
func defaultKeyMap() KeyMap {
	return KeyMap{
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch focus"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
		),
		// Show newline help in footer. Terminals that support Shift+Enter will use it.
		// Ctrl+J acts as a fallback on terminals that don't distinguish Shift+Enter.
		ShiftNewline: key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			key.WithHelp("shift+enter / ctrl+j", "newline"),
		),
		ExternalEditor: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "edit in $EDITOR"),
		),
	}
}

// New creates a new chat page
func New(a *app.App, sessionState *service.SessionState) Page {
	historyStore, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize command history: %v\n", err)
	}

	p := &chatPage{
		sidebar:      sidebar.New(),
		messages:     messages.New(a, sessionState),
		editor:       editor.New(a, historyStore),
		focusedPanel: PanelEditor,
		app:          a,
		keyMap:       defaultKeyMap(),
		history:      historyStore,
		sessionState: sessionState,
		// Default to no keyboard enhancements (will be updated if msg is received)
		keyboardEnhancementsSupported: false,
		editorLines:                   3,
	}

	// Initialize help text with default (ctrl+j)
	p.updateNewlineHelp()

	return p
}

// NewWithSession creates a new chat page with session history loaded
func NewWithSession(a *app.App, sessionState *service.SessionState, sess *session.Session) Page {
	historyStore, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize command history: %v\n", err)
	}

	msg := messages.New(a, sessionState)

	p := &chatPage{
		sidebar:      sidebar.New(),
		messages:     msg,
		editor:       editor.New(a, historyStore),
		focusedPanel: PanelEditor,
		app:          a,
		keyMap:       defaultKeyMap(),
		history:      historyStore,
		sessionState: sessionState,
		// Default to no keyboard enhancements (will be updated if msg is received)
		keyboardEnhancementsSupported: false,
		editorLines:                   3,
	}

	// Initialize help text with default (ctrl+j)
	p.updateNewlineHelp()

	// Load session history
	if sess != nil && len(sess.Messages) > 0 {
		p.loadSessionHistory(sess)
	}

	return p
}

// loadSessionHistory populates the messages component with historical session data
func (p *chatPage) loadSessionHistory(sess *session.Session) {
	// Build a map of tool call IDs to their results for matching
	toolResults := make(map[string]string)
	for _, item := range sess.Messages {
		if item.IsMessage() {
			msg := item.Message
			if msg.Message.Role == chat.MessageRoleTool && msg.Message.ToolCallID != "" {
				toolResults[msg.Message.ToolCallID] = msg.Message.Content
			}
		}
	}

	for _, item := range sess.Messages {
		if item.IsMessage() {
			msg := item.Message
			switch msg.Message.Role {
			case chat.MessageRoleUser:
				if !msg.Implicit {
					p.messages.AddUserMessageSync(msg.Message.Content)
				}
			case chat.MessageRoleAssistant:
				// Add any text content first
				if msg.Message.Content != "" {
					p.messages.AddAssistantMessageSync(msg.AgentName, msg.Message.Content)
				}
				// Then add tool calls
				for _, toolCall := range msg.Message.ToolCalls {
					result := toolResults[toolCall.ID]
					p.messages.AddToolCallSync(msg.AgentName, toolCall, result)
				}
			// Skip tool messages - they are handled above with the assistant message's tool calls
			case chat.MessageRoleTool:
				continue
			}
		} else if item.IsSubSession() {
			// Recursively load sub-session messages
			p.loadSessionHistory(item.SubSession)
		}
	}
}

// Init initializes the chat page
func (p *chatPage) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Add welcome message if present
	welcomeMsg := p.app.CurrentWelcomeMessage(context.Background())
	if welcomeMsg != "" {
		cmds = append(cmds, p.messages.AddWelcomeMessage(welcomeMsg))
	}

	cmds = append(cmds,
		p.sidebar.Init(),
		p.messages.Init(),
		p.editor.Init(),
		p.editor.Focus(),
	)

	return tea.Batch(cmds...)
}

// Update handles messages and updates the page state
func (p *chatPage) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		// Track keyboard enhancement support and update help text
		p.keyboardEnhancementsSupported = msg.Flags != 0
		p.updateNewlineHelp()
		// Forward to editor
		editorModel, editorCmd := p.editor.Update(msg)
		p.editor = editorModel.(editor.Editor)
		return p, editorCmd

	case tea.WindowSizeMsg:
		cmd := p.SetSize(msg.Width, msg.Height)
		cmds = append(cmds, cmd)

		// Forward to sidebar component
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)
		cmds = append(cmds, sidebarCmd)

		// Forward to chat component
		chatModel, chatCmd := p.messages.Update(msg)
		p.messages = chatModel.(messages.Model)
		cmds = append(cmds, chatCmd)

		// Forward to editor component
		editorModel, editorCmd := p.editor.Update(msg)
		p.editor = editorModel.(editor.Editor)
		cmds = append(cmds, editorCmd)
		return p, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		if msg.String() == "tab" && p.focusedPanel == PanelEditor {
			if p.editor.AcceptSuggestion() {
				return p, nil
			}
		}

		if msg.String() == "ctrl+t" {
			model, cmd := p.messages.Update(editfile.ToggleDiffViewMsg{})
			p.messages = model.(messages.Model)
			return p, cmd
		}

		switch {
		case key.Matches(msg, p.keyMap.Tab):
			p.switchFocus()
			return p, nil
		case key.Matches(msg, p.keyMap.Cancel):
			// Cancel current message processing if active
			cmd := p.cancelStream(true)
			return p, cmd
		case key.Matches(msg, p.keyMap.ExternalEditor):
			// Open external editor with current editor content
			cmd := p.openExternalEditor()
			return p, cmd
		}

		// Route other keys to focused component
		switch p.focusedPanel {
		case PanelChat:
			model, cmd := p.messages.Update(msg)
			p.messages = model.(messages.Model)
			return p, cmd
		case PanelEditor:
			model, cmd := p.editor.Update(msg)
			p.editor = model.(editor.Editor)
			return p, cmd
		}

		return p, nil

	case tea.MouseClickMsg:
		if p.isOnResizeHandle(msg.X, msg.Y) {
			p.isDragging = true
			return p, nil
		}
		cmd := p.routeMouseEvent(msg, msg.Y)
		return p, cmd

	case tea.MouseMotionMsg:
		if p.isDragging {
			cmd := p.handleResize(msg.Y)
			return p, cmd
		}
		p.isHoveringHandle = p.isOnResizeLine(msg.Y)
		cmd := p.routeMouseEvent(msg, msg.Y)
		return p, cmd

	case tea.MouseReleaseMsg:
		if p.isDragging {
			p.isDragging = false
			return p, nil
		}
		cmd := p.routeMouseEvent(msg, msg.Y)
		return p, cmd

	case tea.MouseWheelMsg:
		cmd := p.routeMouseEvent(msg, msg.Y)
		return p, cmd

	case editor.SendMsg:
		slog.Debug(msg.Content)
		cmd := p.processMessage(msg)
		return p, cmd

	case messages.StreamCancelledMsg:
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)

		var cmds []tea.Cmd
		cmds = append(cmds, cmd)

		if msg.ShowMessage {
			cmds = append(cmds, p.messages.AddCancelledMessage())
		}
		cmds = append(cmds, p.messages.ScrollToBottom())
		return p, tea.Batch(cmds...)

	// Runtime events
	case *runtime.ErrorEvent:
		cmd := p.messages.AddErrorMessage(msg.Error)
		return p, cmd
	case *runtime.ShellOutputEvent:
		cmd := p.messages.AddShellOutputMessage(msg.Output)
		return p, cmd
	case *runtime.WarningEvent:
		cmd := core.CmdHandler(notification.ShowMsg{Text: msg.Message, Type: notification.TypeWarning})
		return p, cmd
	case *runtime.RAGIndexingStartedEvent, *runtime.RAGIndexingProgressEvent, *runtime.RAGIndexingCompletedEvent:
		// Forward RAG events to sidebar
		slog.Debug("Chat page forwarding RAG event to sidebar", "event_type", fmt.Sprintf("%T", msg))
		var model layout.Model
		var cmd tea.Cmd
		model, cmd = p.sidebar.Update(msg)
		p.sidebar = model.(sidebar.Model)
		return p, cmd
	case *runtime.UserMessageEvent:
		cmd := p.messages.AddUserMessage(msg.Message)
		return p, cmd
	case *runtime.StreamStartedEvent:
		p.streamCancelled = false
		spinnerCmd := p.setWorking(true)
		cmd := p.messages.AddAssistantMessage()
		p.startProgressBar()
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)
		return p, tea.Batch(cmd, spinnerCmd, sidebarCmd)
	case *runtime.AgentChoiceEvent:
		if p.streamCancelled {
			return p, nil
		}
		cmd := p.messages.AppendToLastMessage(msg.AgentName, types.MessageTypeAssistant, msg.Content)
		return p, cmd
	case *runtime.AgentChoiceReasoningEvent:
		if p.streamCancelled {
			return p, nil
		}
		cmd := p.messages.AppendToLastMessage(msg.AgentName, types.MessageTypeAssistantReasoning, msg.Content)
		return p, cmd
	case *runtime.TokenUsageEvent:
		p.sidebar.SetTokenUsage(msg)
	case *runtime.AgentInfoEvent:
		p.sidebar.SetAgentInfo(msg.AgentName, msg.Model, msg.Description)
	case *runtime.TeamInfoEvent:
		p.sidebar.SetTeamInfo(msg.AvailableAgents)
	case *runtime.AgentSwitchingEvent:
		p.sidebar.SetAgentSwitching(msg.Switching)
	case *runtime.ToolsetInfoEvent:
		p.sidebar.SetToolsetInfo(msg.AvailableTools)
	case *runtime.StreamStoppedEvent:
		spinnerCmd := p.setWorking(false)
		if p.msgCancel != nil {
			p.msgCancel = nil
		}
		p.streamCancelled = false
		p.stopProgressBar()
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)
		return p, tea.Batch(p.messages.ScrollToBottom(), spinnerCmd, sidebarCmd)
	case *runtime.SessionTitleEvent:
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)
		return p, sidebarCmd
	case *runtime.PartialToolCallEvent:
		// When we first receive a tool call, show it immediately in pending state
		spinnerCmd := p.setWorking(true)
		cmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusPending)
		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd)
	case *runtime.ToolCallConfirmationEvent:
		spinnerCmd := p.setWorking(false)
		cmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusConfirmation)

		// Open tool confirmation dialog
		dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewToolConfirmationDialog(msg, p.sessionState),
		})

		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd, dialogCmd)
	case *runtime.ToolCallEvent:
		spinnerCmd := p.setWorking(true)
		cmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusRunning)
		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd)
	case *runtime.ToolCallResponseEvent:
		spinnerCmd := p.setWorking(true)
		cmd := p.messages.AddToolResult(msg, types.ToolStatusCompleted)

		// Check if this is a todo-related tool call and update sidebar
		if msg.ToolDefinition.Category == "todo" && !msg.Result.IsError {
			_ = p.sidebar.SetTodos(msg.Result)
		}

		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd)
	case *runtime.MaxIterationsReachedEvent:
		spinnerCmd := p.setWorking(false)

		// Open max iterations confirmation dialog
		dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewMaxIterationsDialog(msg.MaxIterations, p.app),
		})

		return p, tea.Batch(spinnerCmd, dialogCmd)
	case *runtime.ElicitationRequestEvent:
		// TODO: handle normal elicitation requests
		spinnerCmd := p.setWorking(false)

		serverURL := msg.Meta["cagent/server_url"].(string)
		dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewOAuthAuthorizationDialog(serverURL, p.app),
		})

		return p, tea.Batch(spinnerCmd, dialogCmd)
	}

	sidebarModel, sidebarCmd := p.sidebar.Update(msg)
	p.sidebar = sidebarModel.(sidebar.Model)
	cmds = append(cmds, sidebarCmd)

	chatModel, chatCmd := p.messages.Update(msg)
	p.messages = chatModel.(messages.Model)
	cmds = append(cmds, chatCmd)

	editorModel, editorCmd := p.editor.Update(msg)
	p.editor = editorModel.(editor.Editor)
	cmds = append(cmds, editorCmd)

	return p, tea.Batch(cmds...)
}

func (p *chatPage) setWorking(working bool) tea.Cmd {
	return tea.Batch(p.sidebar.SetWorking(working), p.editor.SetWorking(working))
}

// View renders the chat page
func (p *chatPage) View() string {
	// Main chat content area (without input)
	innerWidth := p.width // subtract app style padding

	var bodyContent string

	if p.width >= minWindowWidth {
		chatWidth := innerWidth - sidebarWidth

		chatView := styles.ChatStyle.
			Height(p.chatHeight).
			Width(chatWidth).
			Render(p.messages.View())

		sidebarView := lipgloss.NewStyle().
			Width(sidebarWidth).
			Height(p.chatHeight).
			Align(lipgloss.Left, lipgloss.Top).
			Render(p.sidebar.View())

		bodyContent = lipgloss.JoinHorizontal(
			lipgloss.Left,
			chatView,
			sidebarView,
		)
	} else {
		sidebarWidth, sidebarHeight := p.sidebar.GetSize()

		chatView := styles.ChatStyle.
			Height(p.chatHeight).
			Width(innerWidth).
			Render(p.messages.View())

		sidebarView := lipgloss.NewStyle().
			Width(sidebarWidth).
			Height(sidebarHeight).
			Align(lipgloss.Left, lipgloss.Top).
			Render(p.sidebar.View())

		bodyContent = lipgloss.JoinVertical(
			lipgloss.Top,
			sidebarView,
			chatView,
		)
	}

	// Resize handle between messages and editor
	resizeHandle := p.renderResizeHandle(innerWidth)

	// Input field spans full width below everything
	input := p.editor.View()

	// Create a full-height layout with header, body, resize handle, and input
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		bodyContent,
		resizeHandle,
		input,
	)

	return styles.AppStyle.
		Height(p.height).
		Render(content)
}

func (p *chatPage) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height

	var cmds []tea.Cmd

	// Calculate heights accounting for padding
	// Clamp editor lines between 4 (min) and half screen (max)
	minLines := 4
	maxLines := max(minLines, (height-6)/2) // Leave room for messages
	p.editorLines = max(minLines, min(p.editorLines, maxLines))

	// Account for horizontal padding in width
	innerWidth := width - 2 // subtract left/right padding

	targetEditorHeight := p.editorLines
	editorCmd := p.editor.SetSize(innerWidth, targetEditorHeight)
	cmds = append(cmds, editorCmd)

	_, actualEditorHeight := p.editor.GetSize()
	p.inputHeight = actualEditorHeight

	// Emit height change message so completion popup can adjust position
	cmds = append(cmds, core.CmdHandler(EditorHeightChangedMsg{Height: actualEditorHeight}))

	var mainWidth int
	if width >= minWindowWidth {
		mainWidth = innerWidth - sidebarWidth
		if mainWidth < 1 {
			mainWidth = 1
		}
		p.chatHeight = max(1, height-actualEditorHeight-1) // -1 for resize handle
		p.sidebar.SetMode(sidebar.ModeVertical)
		cmds = append(cmds,
			p.sidebar.SetSize(sidebarWidth, p.chatHeight),
			p.messages.SetPosition(0, 0),
		)
	} else {
		const horizontalSidebarHeight = 3
		mainWidth = innerWidth
		if mainWidth < 1 {
			mainWidth = 1
		}
		p.chatHeight = max(1, height-actualEditorHeight-horizontalSidebarHeight-1) // -1 for resize handle
		p.sidebar.SetMode(sidebar.ModeHorizontal)
		cmds = append(cmds,
			p.sidebar.SetSize(width, horizontalSidebarHeight),
			p.messages.SetPosition(0, horizontalSidebarHeight),
		)
	}

	// Set component sizes
	cmds = append(cmds,
		p.messages.SetSize(mainWidth, p.chatHeight),
	)

	return tea.Batch(cmds...)
}

// GetSize returns the current dimensions
func (p *chatPage) GetSize() (width, height int) {
	return p.width, p.height
}

// GetInputHeight returns the current height of the editor/input area (including padding)
func (p *chatPage) GetInputHeight() int {
	return p.inputHeight
}

// Bindings returns key bindings for the chat page
func (p *chatPage) Bindings() []key.Binding {
	bindings := []key.Binding{
		p.keyMap.Tab,
		p.keyMap.Cancel,
		// show newline hints in the global footer
		p.keyMap.ShiftNewline,
		p.keyMap.ExternalEditor,
	}

	if p.focusedPanel == PanelChat {
		bindings = append(bindings, p.messages.Bindings()...)
	}

	return bindings
}

// Help returns help information
func (p *chatPage) Help() help.KeyMap {
	return core.NewSimpleHelp(p.Bindings())
}

// switchFocus cycles between the focusable panels
func (p *chatPage) switchFocus() {
	p.messages.Blur()
	p.editor.Blur()

	// Move to next panel
	switch p.focusedPanel {
	case PanelChat:
		p.focusedPanel = PanelEditor
		p.editor.Focus()
	case PanelEditor:
		p.focusedPanel = PanelChat
		p.messages.Focus()
	}
}

// updateNewlineHelp updates the help text for the newline shortcut
// based on keyboard enhancement support.
func (p *chatPage) updateNewlineHelp() {
	if p.keyboardEnhancementsSupported {
		// When keyboard enhancements are supported, show both options
		p.keyMap.ShiftNewline = key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			key.WithHelp("shift+enter / ctrl+j", "newline"),
		)
	} else {
		// When not supported, only ctrl+j works
		p.keyMap.ShiftNewline = key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("ctrl+j", "newline"),
		)
	}
}

// cancelStream cancels the current stream and cleans up associated state
func (p *chatPage) cancelStream(showCancelMessage bool) tea.Cmd {
	if p.msgCancel == nil {
		return nil
	}

	p.msgCancel()
	p.msgCancel = nil
	p.streamCancelled = true
	p.stopProgressBar()

	// Send StreamCancelledMsg to all components to handle cleanup
	return tea.Batch(
		core.CmdHandler(messages.StreamCancelledMsg{ShowMessage: showCancelMessage}),
		p.setWorking(false),
	)
}

// processMessage processes a message with the runtime
func (p *chatPage) processMessage(msg editor.SendMsg) tea.Cmd {
	if p.msgCancel != nil {
		p.msgCancel()
	}

	_ = p.history.Add(msg.Content)

	var ctx context.Context
	ctx, p.msgCancel = context.WithCancel(context.Background())

	if strings.HasPrefix(msg.Content, "!") {
		p.app.RunBangCommand(ctx, msg.Content[1:])
		return p.messages.ScrollToBottom()
	}

	// Handle built-in slash commands that support arguments
	if strings.HasPrefix(msg.Content, "/") {
		cmd, rest, _ := strings.Cut(msg.Content, " ")
		switch cmd {
		case "/eval":
			// Trim whitespace from the filename argument
			filename := strings.TrimSpace(rest)
			return core.CmdHandler(msgtypes.EvalSessionMsg{Filename: filename})
		case "/new":
			return core.CmdHandler(msgtypes.NewSessionMsg{})
		case "/compact":
			return core.CmdHandler(msgtypes.CompactSessionMsg{})
		case "/copy":
			return core.CmdHandler(msgtypes.CopySessionToClipboardMsg{})
		case "/yolo":
			return core.CmdHandler(msgtypes.ToggleYoloMsg{})
		case "/sessions":
			return core.CmdHandler(msgtypes.OpenSessionsDialogMsg{})
		}
		// If not a built-in command, fall through to let the app handle it (e.g., agent commands)
	}

	p.app.Run(ctx, p.msgCancel, msg.Content, msg.Attachments)

	return p.messages.ScrollToBottom()
}

// CompactSession generates a summary and compacts the session history
func (p *chatPage) CompactSession() tea.Cmd {
	// Cancel any active stream without showing cancellation message
	p.cancelStream(false)

	p.app.CompactSession()

	return p.messages.ScrollToBottom()
}

func (p *chatPage) Cleanup() {
	p.stopProgressBar()
	p.editor.Cleanup()
}

// routeMouseEvent routes mouse events to editor (bottom) or messages (top) based on Y.
func (p *chatPage) routeMouseEvent(msg tea.Msg, y int) tea.Cmd {
	editorTop := p.height - p.inputHeight
	if y < editorTop {
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return cmd
	}

	// Check for banner clicks to open attachment preview
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		editorTopPadding := styles.EditorStyle.GetPaddingTop()
		localY := y - editorTop - editorTopPadding
		if localY >= 0 && localY < p.editor.BannerHeight() {
			localX := max(0, click.X-styles.AppPaddingLeft)
			if preview, ok := p.editor.AttachmentAt(localX); ok {
				return p.openAttachmentPreview(preview)
			}
		}
	}

	model, cmd := p.editor.Update(msg)
	p.editor = model.(editor.Editor)
	return cmd
}

// isOnResizeLine checks if y is on the resize handle line.
func (p *chatPage) isOnResizeLine(y int) bool {
	// Use current editor height (includes dynamic banner) rather than cached value
	_, editorHeight := p.editor.GetSize()
	return y == p.height-editorHeight-1
}

// isOnResizeHandle checks if (x, y) is on the draggable center of the resize handle.
func (p *chatPage) isOnResizeHandle(x, y int) bool {
	if !p.isOnResizeLine(y) {
		return false
	}
	// Only the center portion is draggable
	center := p.width / 2
	return x >= center-resizeHandleWidth/2 && x < center+resizeHandleWidth/2
}

// handleResize adjusts editor height based on drag position.
func (p *chatPage) handleResize(y int) tea.Cmd {
	// Subtract EditorStyle padding to get internal content lines
	editorPadding := styles.EditorStyle.GetVerticalFrameSize()
	targetLines := p.height - y - 1 - editorPadding
	newLines := max(3, min(targetLines, (p.height-6)/2))
	if newLines != p.editorLines {
		p.editorLines = newLines
		return p.SetSize(p.width, p.height)
	}
	return nil
}

// renderResizeHandle renders the draggable separator between messages and editor.
func (p *chatPage) renderResizeHandle(width int) string {
	if p.isHoveringHandle || p.isDragging {
		// Show a small centered highlight when hovered or dragging
		handleWidth := min(resizeHandleWidth, width)
		sideWidth := (width - handleWidth) / 2
		leftPart := strings.Repeat("─", sideWidth)
		centerPart := strings.Repeat("─", handleWidth)
		rightPart := strings.Repeat("─", width-sideWidth-handleWidth)

		// Use brighter style when actively dragging
		centerStyle := styles.ResizeHandleHoverStyle
		if p.isDragging {
			centerStyle = styles.ResizeHandleActiveStyle
		}

		return styles.ResizeHandleStyle.Render(leftPart) +
			centerStyle.Render(centerPart) +
			styles.ResizeHandleStyle.Render(rightPart)
	}
	// Simple thin line when not hovered
	return styles.ResizeHandleStyle.Render(strings.Repeat("─", width))
}

func (p *chatPage) openAttachmentPreview(preview editor.AttachmentPreview) tea.Cmd {
	return core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewAttachmentPreviewDialog(preview),
	})
}

// See: https://conemu.github.io/en/AnsiEscapeCodes.html#ConEmu_specific_OSC
func (p *chatPage) startProgressBar() {
	fmt.Fprint(os.Stderr, "\x1b]9;4;3;0\x1b\\")
}

func (p *chatPage) stopProgressBar() {
	fmt.Fprint(os.Stderr, "\x1b]9;4;0;0\x1b\\")
}
