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
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/components/spinner"
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
	CompactSession(additionalPrompt string) tea.Cmd
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
	spinner  spinner.Spinner

	sessionState *service.SessionState

	// State
	focusedPanel FocusedPanel
	working      bool

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
	Tab             key.Binding
	Cancel          key.Binding
	ShiftNewline    key.Binding
	CtrlJ           key.Binding
	ExternalEditor  key.Binding
	ToggleSplitDiff key.Binding
}

// defaultKeyMap returns the default key bindings
func defaultKeyMap() KeyMap {
	return KeyMap{
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("TAB", "switch focus"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
		),
		// Show newline help in footer. Terminals that support Shift+Enter will use it.
		// Ctrl+J acts as a fallback on terminals that don't distinguish Shift+Enter.
		ShiftNewline: key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			key.WithHelp("Shift+Enter / Ctrl+j", "newline"),
		),
		ExternalEditor: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("Ctrl+g", "edit in $EDITOR"),
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
		sidebar:      sidebar.New(sessionState),
		messages:     messages.New(a, sessionState),
		editor:       editor.New(a, historyStore),
		spinner:      spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
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
		switch {
		case key.Matches(msg, p.keyMap.Tab):
			if p.focusedPanel == PanelEditor && p.editor.AcceptSuggestion() {
				return p, nil
			}

			p.switchFocus()
			return p, nil

		case key.Matches(msg, p.keyMap.Cancel):
			cmd := p.cancelStream(true)
			return p, cmd

		case key.Matches(msg, p.keyMap.ExternalEditor):
			cmd := p.openExternalEditor()
			return p, cmd

		case key.Matches(msg, p.keyMap.ToggleSplitDiff):
			model, cmd := p.messages.Update(editfile.ToggleDiffViewMsg{})
			p.messages = model.(messages.Model)
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

	case msgtypes.ToggleHideToolResultsMsg:
		// Forward to messages component to invalidate cache and trigger redraw
		model, cmd := p.messages.Update(messages.ToggleHideToolResultsMsg{})
		p.messages = model.(messages.Model)
		return p, cmd

	// Runtime events
	case *runtime.ErrorEvent:
		cmd := p.messages.AddErrorMessage(msg.Error)
		return p, cmd
	case *runtime.ShellOutputEvent:
		cmd := p.messages.AddShellOutputMessage(msg.Output)
		return p, cmd
	case *runtime.WarningEvent:
		return p, notification.WarningCmd(msg.Message)
	case *runtime.RAGIndexingStartedEvent, *runtime.RAGIndexingProgressEvent, *runtime.RAGIndexingCompletedEvent:
		// Forward RAG events to sidebar
		slog.Debug("Chat page forwarding RAG event to sidebar", "event_type", fmt.Sprintf("%T", msg))
		model, cmd := p.sidebar.Update(msg)
		p.sidebar = model.(sidebar.Model)
		return p, cmd
	case *runtime.UserMessageEvent:
		// Replace loading message with actual user message (if there was one)
		cmd := p.messages.ReplaceLoadingWithUser(msg.Message)
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
	case *runtime.SessionCompactionEvent:
		if msg.Status == "completed" {
			return p, notification.SuccessCmd("Session compacted successfully.")
		}
	case *runtime.AgentInfoEvent:
		p.sidebar.SetAgentInfo(msg.AgentName, msg.Model, msg.Description)
		p.messages.AddWelcomeMessage(msg.WelcomeMessage)
	case *runtime.TeamInfoEvent:
		p.sidebar.SetTeamInfo(msg.AvailableAgents)
	case *runtime.AgentSwitchingEvent:
		p.sidebar.SetAgentSwitching(msg.Switching)
		// Track sub-agent execution for transfer_task tree view
		p.messages.SetSubAgentActive(msg.Switching, msg.ToAgent)
	case *runtime.ToolsetInfoEvent:
		p.sidebar.SetToolsetInfo(msg.AvailableTools, msg.Loading)
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

		var cmd tea.Cmd
		if msg.Result.IsError {
			cmd = p.messages.AddToolResult(msg, types.ToolStatusError)
		} else {
			cmd = p.messages.AddToolResult(msg, types.ToolStatusCompleted)
		}

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

	if p.working {
		model, cmd := p.spinner.Update(msg)
		p.spinner = model.(spinner.Spinner)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

func (p *chatPage) setWorking(working bool) tea.Cmd {
	p.working = working

	cmd := []tea.Cmd{p.editor.SetWorking(working)}
	if working {
		cmd = append(cmd, p.spinner.Init())
	}

	return tea.Batch(cmd...)
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

	targetEditorHeight := p.editorLines - 1
	editorCmd := p.editor.SetSize(innerWidth, targetEditorHeight)
	cmds = append(cmds, editorCmd)

	_, actualEditorHeight := p.editor.GetSize()
	p.inputHeight = actualEditorHeight

	// Emit height change message so completion popup can adjust position
	cmds = append(cmds, core.CmdHandler(EditorHeightChangedMsg{Height: actualEditorHeight}))

	var mainWidth int
	if width >= minWindowWidth {
		mainWidth = max(innerWidth-sidebarWidth, 1)
		p.chatHeight = max(1, height-actualEditorHeight-2) // -1 for resize handle, -1 for empty line before status bar
		p.sidebar.SetMode(sidebar.ModeVertical)
		cmds = append(cmds,
			p.sidebar.SetSize(sidebarWidth, p.chatHeight),
			p.messages.SetPosition(0, 0),
		)
	} else {
		const horizontalSidebarHeight = 3
		mainWidth = max(innerWidth, 1)
		p.chatHeight = max(1, height-actualEditorHeight-horizontalSidebarHeight-2) // -1 for resize handle, -1 for empty line before status bar
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

	// Handle slash commands (e.g., /eval, /compact, /exit)
	if cmd := commands.ParseSlashCommand(msg.Content); cmd != nil {
		return cmd
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
	// Use brighter style when actively dragging
	centerStyle := styles.ResizeHandleHoverStyle
	if p.isDragging {
		centerStyle = styles.ResizeHandleActiveStyle
	}

	// Show a small centered highlight when hovered or dragging
	centerPart := strings.Repeat("─", min(resizeHandleWidth, width))
	handle := centerStyle.Render(centerPart)

	// Add working spinner on the right side
	var suffix string
	if p.working {
		suffix = " " + p.spinner.View() + " " + styles.SpinnerDotsHighlightStyle.Render("Working…")
	}

	return lipgloss.PlaceHorizontal(
		width-2-lipgloss.Width(suffix), lipgloss.Center, handle,
		lipgloss.WithWhitespaceChars("─"),
		lipgloss.WithWhitespaceStyle(styles.ResizeHandleStyle),
	) + suffix
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
