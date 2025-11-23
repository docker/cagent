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
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
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
)

// Page represents the main chat page
type Page interface {
	layout.Model
	layout.Sizeable
	layout.Help
	CompactSession() tea.Cmd
	Cleanup()
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
}

// KeyMap defines key bindings for the chat page
type KeyMap struct {
	Tab          key.Binding
	Cancel       key.Binding
	ShiftNewline key.Binding
	CtrlJ        key.Binding
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
	}
}

// New creates a new chat page
func New(a *app.App, sessionState *service.SessionState) Page {
	historyStore, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize command history: %v\n", err)
	}

	return &chatPage{
		sidebar:      sidebar.New(sessionState.TodoManager),
		messages:     messages.New(a, sessionState),
		editor:       editor.New(a, historyStore),
		focusedPanel: PanelEditor,
		app:          a,
		keyMap:       defaultKeyMap(),
		history:      historyStore,
		sessionState: sessionState,
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

	case tea.MouseWheelMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		// Always forward mouse wheel events to the chat component for scrolling
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return p, cmd

	case editor.SendMsg:
		cmd := p.processMessage(msg.Content)
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
	case *runtime.RAGIndexingStartedEvent, *runtime.RAGIndexingProgressEvent, *runtime.RAGIndexingCompletedEvent, *runtime.RAGReadyEvent:
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
	case *runtime.ToolStatusEvent:
		p.sidebar.SetToolStatus(msg.ToolName, msg.Status)
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
		if msg.ToolDefinition.Category == "todo" {
			// Only update if the response doesn't contain an error
			// Response starting with "Error calling tool:" indicates failure
			// TODO: We should maybe use the mcp types, they have an "IsError" field.
			if len(msg.Response) < 19 || msg.Response[:19] != "Error calling tool:" {
				_ = p.sidebar.SetTodos(msg.ToolCall)
			}
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

	// Input field spans full width below everything
	input := p.editor.View()

	// Create a full-height layout with header, body, and input
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		bodyContent,
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
	editorHeight := 3 // fixed 3 lines for multi-line input

	// Calculate available space, ensuring status bar remains visible
	p.inputHeight = editorHeight + 3 // account for editor padding

	// Account for horizontal padding in width
	innerWidth := width - 2 // subtract left/right padding

	var mainWidth int
	if width >= minWindowWidth {
		mainWidth = innerWidth - sidebarWidth
		p.chatHeight = height - p.inputHeight
		p.sidebar.SetMode(sidebar.ModeVertical)
		cmds = append(cmds, p.sidebar.SetSize(sidebarWidth, p.chatHeight), p.messages.SetPosition(0, 0))
	} else {
		const horizontalSidebarHeight = 3
		mainWidth = innerWidth
		p.chatHeight = height - p.inputHeight - horizontalSidebarHeight
		p.sidebar.SetMode(sidebar.ModeHorizontal)
		cmds = append(cmds, p.sidebar.SetSize(width, horizontalSidebarHeight), p.messages.SetPosition(0, horizontalSidebarHeight))
	}

	// Set component sizes
	cmds = append(cmds,
		p.messages.SetSize(mainWidth, p.chatHeight),
		p.editor.SetSize(innerWidth, editorHeight), // Use calculated editor height
	)

	return tea.Batch(cmds...)
}

// GetSize returns the current dimensions
func (p *chatPage) GetSize() (width, height int) {
	return p.width, p.height
}

// Bindings returns key bindings for the chat page
func (p *chatPage) Bindings() []key.Binding {
	bindings := []key.Binding{
		p.keyMap.Tab,
		p.keyMap.Cancel,
		// show newline hints in the global footer
		p.keyMap.ShiftNewline,
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
func (p *chatPage) processMessage(content string) tea.Cmd {
	if p.msgCancel != nil {
		p.msgCancel()
	}

	var ctx context.Context
	ctx, p.msgCancel = context.WithCancel(context.Background())

	if strings.HasPrefix(content, "!") {
		p.app.RunBangCommand(ctx, content[1:])
		return p.messages.ScrollToBottom()
	}

	// Persist to history
	if p.history != nil {
		if err := p.history.Add(content); err != nil {
			fmt.Fprintf(os.Stderr, "failed to persist command history: %v\n", err)
		}
	}

	p.app.Run(ctx, p.msgCancel, content)

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
}

// See: https://conemu.github.io/en/AnsiEscapeCodes.html#ConEmu_specific_OSC
func (p *chatPage) startProgressBar() {
	fmt.Fprint(os.Stderr, "\x1b]9;4;3;0\x1b\\")
}

func (p *chatPage) stopProgressBar() {
	fmt.Fprint(os.Stderr, "\x1b]9;4;0;0\x1b\\")
}
