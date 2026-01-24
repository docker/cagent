package chat

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	msgtypes "github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
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
	CompactSession(additionalPrompt string) tea.Cmd
	Cleanup()
	// GetInputHeight returns the current height of the editor/input area (including padding)
	GetInputHeight() int
	// SetSessionStarred updates the sidebar star indicator
	SetSessionStarred(starred bool)
	// InsertText inserts text at the current cursor position in the editor
	InsertText(text string)
	// SetRecording sets the recording mode on the editor
	SetRecording(recording bool) tea.Cmd
	// SendEditorContent sends the current editor content as a message
	SendEditorContent() tea.Cmd
}

// queuedMessage represents a message waiting to be sent to the agent
type queuedMessage struct {
	content     string
	attachments map[string]string
}

// maxQueuedMessages is the maximum number of messages that can be queued
const maxQueuedMessages = 5

// chatPage implements Page
type chatPage struct {
	width, height int

	// Components
	sidebar  sidebar.Model
	messages messages.Model
	editor   editor.Editor
	spinner  spinner.Spinner

	sessionState *service.SessionState

	// State
	focusedPanel FocusedPanel
	working      bool

	msgCancel       context.CancelFunc
	streamCancelled bool

	// Track whether we've received content from an assistant response
	// Used by --exit-after-response to ensure we don't exit before receiving content
	hasReceivedAssistantContent bool

	// pendingResponse indicates we're waiting for the first chunk from the model.
	// When true, a spinner is rendered below the messages (outside the message list)
	// to avoid list-wide invalidation on each tick.
	pendingResponse bool
	// pendingSpinner is a dedicated spinner for the pending response indicator.
	// Uses ModeBoth with funny phrases to match the original message spinner style.
	pendingSpinner spinner.Spinner

	// Message queue for enqueuing messages while agent is working
	messageQueue []queuedMessage

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
	Tab             key.Binding
	Cancel          key.Binding
	ShiftNewline    key.Binding
	CtrlJ           key.Binding
	ExternalEditor  key.Binding
	ToggleSplitDiff key.Binding
}

// getEditorDisplayNameFromEnv returns a friendly display name for the configured editor.
// It takes visual and editorEnv values as parameters and maps common editors to display names.
// If neither is set, it returns the platform-specific fallback that will actually be used.
func getEditorDisplayNameFromEnv(visual, editorEnv string) string {
	editorCmd := cmp.Or(visual, editorEnv)
	if editorCmd == "" {
		// Return the actual fallback editor that will be used
		if runtime.GOOS == "windows" {
			return "Notepad"
		}
		return "Vi"
	}

	// Parse the command (may include arguments like "code --wait")
	parts := strings.Fields(editorCmd)
	if len(parts) == 0 {
		return "$EDITOR"
	}

	// Get the base command name (e.g., "/usr/local/bin/code" → "code")
	baseName := filepath.Base(parts[0])

	// Map common editor command prefixes to friendly display names
	// Using prefix matching to handle variants like "code-insiders", "nvim-qt", etc.
	editorPrefixes := []struct {
		prefix string
		name   string
	}{
		{"code", "VSCode"},
		{"cursor", "Cursor"},
		{"nvim", "Neovim"},
		{"vim", "Vim"},
		{"vi", "Vi"},
		{"nano", "Nano"},
		{"emacs", "Emacs"},
		{"subl", "Sublime Text"},
		{"sublime", "Sublime Text"},
		{"atom", "Atom"},
		{"gedit", "gedit"},
		{"kate", "Kate"},
		{"notepad++", "Notepad++"},
		{"notepad", "Notepad"},
		{"textmate", "TextMate"},
		{"mate", "TextMate"},
		{"zed", "Zed"},
	}

	for _, editor := range editorPrefixes {
		if strings.HasPrefix(baseName, editor.prefix) {
			return editor.name
		}
	}

	// Return the base name with first letter capitalized
	if baseName != "" {
		return strings.ToUpper(baseName[:1]) + baseName[1:]
	}

	return "$EDITOR"
}

// getEditorDisplayName returns a friendly display name for the configured editor.
// It reads the VISUAL or EDITOR environment variables and maps common editors to display names.
// If neither is set, it returns the platform-specific fallback that will actually be used.
func getEditorDisplayName() string {
	return getEditorDisplayNameFromEnv(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
}

// defaultKeyMap returns the default key bindings
func defaultKeyMap() KeyMap {
	editorName := getEditorDisplayName()

	return KeyMap{
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "switch focus"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "interrupt"),
		),
		// Show newline help in footer. Terminals that support Shift+Enter will use it.
		// Ctrl+J acts as a fallback on terminals that don't distinguish Shift+Enter.
		ShiftNewline: key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			key.WithHelp("Shift+Enter / Ctrl+j", "newline"),
		),
		ExternalEditor: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("Ctrl+g", fmt.Sprintf("edit in %s", editorName)),
		),
		ToggleSplitDiff: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("Ctrl+t", "toggle split diff mode"),
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
		sidebar:        sidebar.New(sessionState),
		messages:       messages.New(sessionState),
		editor:         editor.New(a, historyStore),
		spinner:        spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		pendingSpinner: spinner.New(spinner.ModeBoth, styles.SpinnerDotsAccentStyle),
		focusedPanel:   PanelEditor,
		app:            a,
		keyMap:         defaultKeyMap(),
		history:        historyStore,
		sessionState:   sessionState,
		// Default to no keyboard enhancements (will be updated if msg is received)
		keyboardEnhancementsSupported: false,
		editorLines:                   3,
	}

	// Initialize help text with default (ctrl+j)
	p.updateNewlineHelp()

	return p
}

// Init initializes the chat page
func (p *chatPage) Init() tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds,
		p.sidebar.Init(),
		p.messages.Init(),
		p.editor.Init(),
		p.editor.Focus(),
	)

	// Load messages from existing session (for session restore)
	if sess := p.app.Session(); sess != nil && len(sess.Messages) > 0 {
		cmds = append(cmds, p.messages.LoadFromSession(sess))
		p.sidebar.LoadFromSession(sess)
	}

	return tea.Batch(cmds...)
}

// Update handles messages and updates the page state
func (p *chatPage) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
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
		cmdSize := p.SetSize(msg.Width, msg.Height)

		// Forward to sidebar component
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)

		// Forward to chat component
		chatModel, chatCmd := p.messages.Update(msg)
		p.messages = chatModel.(messages.Model)

		// Forward to editor component
		editorModel, editorCmd := p.editor.Update(msg)
		p.editor = editorModel.(editor.Editor)
		return p, tea.Batch(cmdSize, sidebarCmd, chatCmd, editorCmd)

	case tea.KeyPressMsg:
		if model, cmd, handled := p.handleKeyPress(msg); handled {
			return model, cmd
		}

	case tea.MouseClickMsg:
		return p.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return p.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return p.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return p.handleMouseWheel(msg)

	case msgtypes.StreamCancelledMsg:
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)

		// Forward to sidebar to stop its spinners
		sidebarModel, sidebarCmd := p.sidebar.Update(msg)
		p.sidebar = sidebarModel.(sidebar.Model)

		var cmds []tea.Cmd
		cmds = append(cmds, cmd, sidebarCmd)

		if msg.ShowMessage {
			cmds = append(cmds, p.messages.AddCancelledMessage())
		}
		cmds = append(cmds, p.messages.ScrollToBottom())

		// Process next queued message after cancel (queue is preserved)
		if queueCmd := p.processNextQueuedMessage(); queueCmd != nil {
			cmds = append(cmds, queueCmd)
		}

		return p, tea.Batch(cmds...)

	case msgtypes.SendMsg:
		slog.Debug(msg.Content)
		return p.handleSendMsg(msg)

	case msgtypes.InsertFileRefMsg:
		// Attach file using editor's AttachFile method which registers the attachment
		p.editor.AttachFile(msg.FilePath)
		return p, nil

	case msgtypes.ToggleHideToolResultsMsg:
		// Forward to messages component to invalidate cache and trigger redraw
		model, cmd := p.messages.Update(messages.ToggleHideToolResultsMsg{})
		p.messages = model.(messages.Model)
		return p, cmd

	case msgtypes.ClearQueueMsg:
		return p.handleClearQueue()

	default:
		// Try to handle as a runtime event
		if handled, cmd := p.handleRuntimeEvent(msg); handled {
			return p, cmd
		}
	}

	sidebarModel, sidebarCmd := p.sidebar.Update(msg)
	p.sidebar = sidebarModel.(sidebar.Model)

	chatModel, chatCmd := p.messages.Update(msg)
	p.messages = chatModel.(messages.Model)

	editorModel, editorCmd := p.editor.Update(msg)
	p.editor = editorModel.(editor.Editor)

	var cmdSpinner tea.Cmd
	if p.working {
		var model layout.Model
		model, cmdSpinner = p.spinner.Update(msg)
		p.spinner = model.(spinner.Spinner)
	}

	var cmdPendingSpinner tea.Cmd
	if p.pendingResponse {
		var model layout.Model
		model, cmdPendingSpinner = p.pendingSpinner.Update(msg)
		p.pendingSpinner = model.(spinner.Spinner)
	}

	return p, tea.Batch(sidebarCmd, chatCmd, editorCmd, cmdSpinner, cmdPendingSpinner)
}

func (p *chatPage) setWorking(working bool) tea.Cmd {
	p.working = working

	cmd := []tea.Cmd{p.editor.SetWorking(working)}
	if working {
		cmd = append(cmd, p.spinner.Init())
	}

	return tea.Batch(cmd...)
}

// setPendingResponse sets the pending response state.
// When true, a spinner is shown below the messages while waiting for the first chunk.
func (p *chatPage) setPendingResponse(pending bool) tea.Cmd {
	p.pendingResponse = pending
	if pending {
		// Reset spinner to get a fresh random phrase
		p.pendingSpinner = p.pendingSpinner.Reset()
		return p.pendingSpinner.Init()
	}
	return nil
}

// View renders the chat page
func (p *chatPage) View() string {
	// Main chat content area (without input)
	// Account for app padding to match SetSize calculations
	// AppStyle adds 2 characters of padding (left=1, right=1)
	innerWidth := p.width - 2 // subtract left/right padding

	var bodyContent string

	// Build messages view with optional pending response spinner
	messagesView := p.messages.View()
	if p.pendingResponse {
		// Append pending spinner below the messages (outside the message list)
		// Uses pendingSpinner which has ModeBoth with funny phrases to match original style
		pendingIndicator := p.pendingSpinner.View()
		if messagesView != "" {
			messagesView = messagesView + "\n\n" + pendingIndicator
		} else {
			messagesView = pendingIndicator
		}
	}

	if p.width >= minWindowWidth {
		// Ensure we don't exceed available space
		chatWidth := max(1, innerWidth-sidebarWidth)

		chatView := styles.ChatStyle.
			Height(p.chatHeight).
			Width(chatWidth).
			Render(messagesView)

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
			Render(messagesView)

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

	targetEditorHeight := p.editorLines - 1
	editorCmd := p.editor.SetSize(innerWidth, targetEditorHeight)
	cmds = append(cmds, editorCmd)

	_, actualEditorHeight := p.editor.GetSize()
	p.inputHeight = actualEditorHeight

	// Emit height change message so completion popup can adjust position
	cmds = append(cmds, core.CmdHandler(EditorHeightChangedMsg{Height: actualEditorHeight}))

	var mainWidth int
	if width >= minWindowWidth {
		// Ensure we don't exceed available space after accounting for sidebar
		mainWidth = max(1, innerWidth-sidebarWidth)
		p.chatHeight = max(1, height-actualEditorHeight-2) // -1 for resize handle, -1 for empty line before status bar
		p.sidebar.SetMode(sidebar.ModeVertical)
		cmds = append(cmds,
			p.sidebar.SetSize(sidebarWidth, p.chatHeight),
			p.sidebar.SetPosition(styles.AppPaddingLeft+mainWidth, 0),
			p.messages.SetPosition(0, 0),
		)
	} else {
		const horizontalSidebarHeight = 3
		mainWidth = max(innerWidth, 1)
		p.chatHeight = max(1, height-actualEditorHeight-horizontalSidebarHeight-2) // -1 for resize handle, -1 for empty line before status bar
		p.sidebar.SetMode(sidebar.ModeHorizontal)
		cmds = append(cmds,
			p.sidebar.SetSize(innerWidth, horizontalSidebarHeight),
			p.sidebar.SetPosition(styles.AppPaddingLeft, 0),
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
	}

	if p.focusedPanel == PanelChat {
		bindings = append(bindings, p.messages.Bindings()...)
	} else {
		bindings = append(bindings,
			p.keyMap.ShiftNewline,
			p.keyMap.ExternalEditor,
		)
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
		p.keyMap.ShiftNewline = key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			key.WithHelp("Shift+Enter", "newline"),
		)
	} else {
		p.keyMap.ShiftNewline = key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("Ctrl+j", "newline"),
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
	p.setPendingResponse(false)
	p.stopProgressBar()

	// Send StreamCancelledMsg to all components to handle cleanup
	return tea.Batch(
		core.CmdHandler(msgtypes.StreamCancelledMsg{ShowMessage: showCancelMessage}),
		p.setWorking(false),
	)
}

// handleSendMsg handles incoming messages from the editor, either processing
// them immediately or queuing them if the agent is busy.
func (p *chatPage) handleSendMsg(msg msgtypes.SendMsg) (layout.Model, tea.Cmd) {
	// Predefined slash commands (e.g., /yolo, /exit, /compact) execute immediately
	// even while the agent is working - they're UI commands that don't interrupt the stream.
	// Custom agent commands (defined in config) should still be queued.
	if commands.ParseSlashCommand(msg.Content) != nil {
		cmd := p.processMessage(msg)
		return p, cmd
	}

	// If not working, process immediately
	if !p.working {
		cmd := p.processMessage(msg)
		return p, cmd
	}

	// If queue is full, reject the message
	if len(p.messageQueue) >= maxQueuedMessages {
		return p, notification.WarningCmd(fmt.Sprintf("Queue full (max %d messages). Please wait.", maxQueuedMessages))
	}

	// Add to queue
	p.messageQueue = append(p.messageQueue, queuedMessage{
		content:     msg.Content,
		attachments: msg.Attachments,
	})
	p.syncQueueToSidebar()

	queueLen := len(p.messageQueue)
	notifyMsg := fmt.Sprintf("Message queued (%d waiting) · Ctrl+X to clear", queueLen)

	return p, notification.InfoCmd(notifyMsg)
}

// processNextQueuedMessage pops the next message from the queue and processes it.
// Returns nil if the queue is empty.
func (p *chatPage) processNextQueuedMessage() tea.Cmd {
	if len(p.messageQueue) == 0 {
		return nil
	}

	// Pop the first message from the queue
	queued := p.messageQueue[0]
	p.messageQueue[0] = queuedMessage{} // zero out to allow GC
	p.messageQueue = p.messageQueue[1:]
	p.syncQueueToSidebar()

	msg := msgtypes.SendMsg{
		Content:     queued.content,
		Attachments: queued.attachments,
	}

	return p.processMessage(msg)
}

// handleClearQueue clears all queued messages and shows a notification.
func (p *chatPage) handleClearQueue() (layout.Model, tea.Cmd) {
	count := len(p.messageQueue)
	if count == 0 {
		return p, notification.InfoCmd("No messages queued")
	}

	p.messageQueue = nil
	p.syncQueueToSidebar()

	var msg string
	if count == 1 {
		msg = "Cleared 1 queued message"
	} else {
		msg = fmt.Sprintf("Cleared %d queued messages", count)
	}
	return p, notification.SuccessCmd(msg)
}

// syncQueueToSidebar updates the sidebar with truncated previews of queued messages.
func (p *chatPage) syncQueueToSidebar() {
	previews := make([]string, len(p.messageQueue))
	for i, qm := range p.messageQueue {
		// Take first line and limit length for preview
		content := strings.TrimSpace(qm.content)
		if idx := strings.IndexAny(content, "\n\r"); idx != -1 {
			content = content[:idx]
		}
		previews[i] = content
	}
	p.sidebar.SetQueuedMessages(previews...)
}

// processMessage processes a message with the runtime
func (p *chatPage) processMessage(msg msgtypes.SendMsg) tea.Cmd {
	// Handle slash commands (e.g., /eval, /compact, /exit) BEFORE cancelling any ongoing stream.
	// These are UI commands that shouldn't interrupt the running agent.
	if cmd := commands.ParseSlashCommand(msg.Content); cmd != nil {
		return cmd
	}

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

	// Start working state immediately to show the user something is happening.
	// This provides visual feedback while the runtime loads tools and prepares the stream.
	// The spinner will be visible in the resize handle area.
	// We start this BEFORE command resolution since that can involve tool execution.
	spinnerCmd := p.setWorking(true)
	p.startProgressBar()

	// Check if this is an agent command that needs resolution
	// If so, show a loading message with the command description
	var loadingCmd tea.Cmd
	if strings.HasPrefix(msg.Content, "/") {
		cmdName, _, _ := strings.Cut(msg.Content[1:], " ")
		if cmd, found := p.app.CurrentAgentCommands(ctx)[cmdName]; found {
			loadingCmd = p.messages.AddLoadingMessage(cmd.DisplayText())
		}
	}

	// Run command resolution and agent execution in a goroutine
	// so the UI can update with the spinner before any blocking operations.
	go func() {
		// Resolve agent commands (e.g., /fix-lint -> prompt text)
		// This can execute tools and take time, but the spinner is already showing.
		resolvedContent := p.app.ResolveCommand(ctx, msg.Content)

		p.app.Run(ctx, p.msgCancel, resolvedContent, msg.Attachments)
	}()

	return tea.Batch(p.messages.ScrollToBottom(), spinnerCmd, loadingCmd)
}

// CompactSession generates a summary and compacts the session history
func (p *chatPage) CompactSession(additionalPrompt string) tea.Cmd {
	// Cancel any active stream without showing cancellation message
	p.cancelStream(false)

	p.app.CompactSession(additionalPrompt)

	return p.messages.ScrollToBottom()
}

func (p *chatPage) Cleanup() {
	p.stopProgressBar()
	p.editor.Cleanup()
}

// SetSessionStarred updates the sidebar star indicator
func (p *chatPage) SetSessionStarred(starred bool) {
	p.sidebar.SetSessionStarred(starred)
}

// handleSidebarClick checks if a click in the sidebar area should toggle the star
// Returns true if the click was handled (star was toggled)
func (p *chatPage) handleSidebarClick(x, y int) bool {
	// Account for AppStyle padding (left padding = 1)
	adjustedX := x - styles.AppPaddingLeft

	if p.width < minWindowWidth {
		// Horizontal mode - sidebar is at the top (y=0 to sidebarHeight)
		// The sidebar view is rendered directly without additional offsets
		return p.sidebar.HandleClick(adjustedX, y)
	}

	// Vertical mode - sidebar is on the right side
	innerWidth := p.width - 2 // subtract left/right padding from AppStyle
	chatWidth := max(1, innerWidth-sidebarWidth)

	// Check if click is in the sidebar area (right side)
	// Sidebar now owns its own left padding via layoutCfg
	if adjustedX >= chatWidth {
		// Calculate x relative to sidebar's outer boundary
		sidebarX := adjustedX - chatWidth
		return p.sidebar.HandleClick(sidebarX, y)
	}
	return false
}

// routeMouseEvent routes mouse events to editor (bottom), sidebar (right), or messages (top-left) based on coordinates.
func (p *chatPage) routeMouseEvent(msg tea.Msg, y int) tea.Cmd {
	editorTop := p.height - p.inputHeight
	if y < editorTop {
		// Check if event is in sidebar area (vertical mode only)
		if p.width >= minWindowWidth {
			// Get x coordinate from the message
			var x int
			switch m := msg.(type) {
			case tea.MouseClickMsg:
				x = m.X
			case tea.MouseMotionMsg:
				x = m.X
			case tea.MouseReleaseMsg:
				x = m.X
			}

			adjustedX := x - styles.AppPaddingLeft
			innerWidth := p.width - 2
			chatWidth := max(1, innerWidth-sidebarWidth)

			// Route to sidebar if in sidebar area
			if adjustedX >= chatWidth {
				model, cmd := p.sidebar.Update(msg)
				p.sidebar = model.(sidebar.Model)
				return cmd
			}
		}

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
	return y == p.height-editorHeight-2
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
	// Guard against zero or negative width (can happen before WindowSizeMsg is received)
	if width <= 0 {
		return ""
	}

	// Use brighter style when actively dragging
	centerStyle := styles.ResizeHandleHoverStyle
	if p.isDragging {
		centerStyle = styles.ResizeHandleActiveStyle
	}

	// Show a small centered highlight when hovered or dragging
	centerPart := strings.Repeat("─", min(resizeHandleWidth, width))
	handle := centerStyle.Render(centerPart)

	// Always center handle on full width
	fullLine := lipgloss.PlaceHorizontal(
		max(0, width-2), lipgloss.Center, handle,
		lipgloss.WithWhitespaceChars("─"),
		lipgloss.WithWhitespaceStyle(styles.ResizeHandleStyle),
	)

	if p.working {
		// Truncate right side and append spinner (handle stays centered)
		workingText := "Working…"
		if queueLen := len(p.messageQueue); queueLen > 0 {
			workingText = fmt.Sprintf("Working… (%d queued)", queueLen)
		}
		suffix := " " + p.spinner.View() + " " + styles.SpinnerDotsHighlightStyle.Render(workingText)
		cancelKeyPart := styles.HighlightWhiteStyle.Render(p.keyMap.Cancel.Help().Key)
		suffix += " (" + cancelKeyPart + " to interrupt)"
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(width - 2 - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	// Show queue count even when not working (messages waiting to be processed)
	if queueLen := len(p.messageQueue); queueLen > 0 {
		queueText := fmt.Sprintf("%d queued", queueLen)
		suffix := " " + styles.WarningStyle.Render(queueText) + " "
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(width - 2 - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	return fullLine
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

// InsertText inserts text at the current cursor position in the editor
func (p *chatPage) InsertText(text string) {
	p.editor.InsertText(text)
}

// SetRecording sets the recording mode on the editor
func (p *chatPage) SetRecording(recording bool) tea.Cmd {
	return p.editor.SetRecording(recording)
}

// SendEditorContent sends the current editor content as a message
func (p *chatPage) SendEditorContent() tea.Cmd {
	return p.editor.SendContent()
}
