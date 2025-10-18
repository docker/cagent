package chat

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// FocusedPanel represents which panel is currently focused
type FocusedPanel string

const (
	PanelChat   FocusedPanel = "chat"
	PanelEditor FocusedPanel = "editor"
)

// Page represents the main chat page
type Page interface {
	layout.Model
	layout.Sizeable
	layout.Help
	CompactSession() tea.Cmd
	CopySessionToClipboard() tea.Cmd
	Cleanup()
}

// chatPage implements Page
type chatPage struct {
	width, height int
	sessionTitle  string

	// Components
	sidebar  sidebar.Model
	messages messages.Model
	editor   editor.Editor

	// State
	focusedPanel FocusedPanel

	msgCancel context.CancelFunc

	// Key map
	keyMap KeyMap

	title string
	app   *app.App

	// Cached layout dimensions
	chatHeight  int
	inputHeight int
}

// KeyMap defines key bindings for the chat page
type KeyMap struct {
	Tab    key.Binding
	Cancel key.Binding
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
			key.WithHelp("esc", "cancel stream"),
		),
	}
}

// New creates a new chat page
func New(a *app.App) Page {
	return &chatPage{
		title:        a.Title(),
		sidebar:      sidebar.New(),
		messages:     messages.New(a),
		editor:       editor.New(),
		focusedPanel: PanelEditor,
		app:          a,
		keyMap:       defaultKeyMap(),
	}
}

// Init initializes the chat page
func (p *chatPage) Init() tea.Cmd {
	cmds := []tea.Cmd{
		p.sidebar.Init(),
		p.messages.Init(),
		p.editor.Init(),
		p.editor.Focus(),
	}

	if firstMessage := p.app.FirstMessage(); firstMessage != nil {
		cmds = append(cmds, func() tea.Msg {
			return editor.SendMsg{
				Content: *firstMessage,
			}
		})
	}

	return tea.Batch(cmds...)
}

// Update handles messages and updates the page state
func (p *chatPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		switch {
		case key.Matches(msg, p.keyMap.Tab):
			p.switchFocus()
			return p, nil
		case key.Matches(msg, p.keyMap.Cancel):
			// Cancel current message processing if active
			if p.msgCancel != nil {
				p.msgCancel()
				p.msgCancel = nil
			}
			// Stop progress bar if active
			p.stopProgressBar()
			return p, nil
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

	case tea.MouseWheelMsg:
		// Always forward mouse wheel events to the chat component for scrolling
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return p, cmd

	case editor.SendMsg:
		cmd := p.processMessage(msg.Content)
		return p, cmd

	// Runtime events
	case *runtime.ErrorEvent:
		cmd := p.messages.AddErrorMessage(msg.Error)
		return p, tea.Batch(cmd, p.messages.ScrollToBottom())
	case *runtime.MCPInitStartedEvent:
		spinnerCmd := p.sidebar.SetMCPInitializing(true)
		return p, spinnerCmd
	case *runtime.MCPInitFinishedEvent:
		spinnerCmd := p.sidebar.SetMCPInitializing(false)
		return p, spinnerCmd
	case *runtime.UserMessageEvent:
		cmd := p.messages.AddUserMessage(msg.Message)
		return p, tea.Batch(cmd, p.messages.ScrollToBottom())
	case *runtime.StreamStartedEvent:
		spinnerCmd := p.setWorking(true)
		cmd := p.messages.AddAssistantMessage()
		p.startProgressBar()
		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd)
	case *runtime.AgentChoiceEvent:
		cmd := p.messages.AppendToLastMessage(msg.AgentName, types.MessageTypeAssistant, msg.Content)
		if p.messages.IsAtBottom() {
			return p, tea.Batch(cmd, p.messages.ScrollToBottom())
		}
		return p, cmd
	case *runtime.AgentChoiceReasoningEvent:
		cmd := p.messages.AppendToLastMessage(msg.AgentName, types.MessageTypeAssistantReasoning, msg.Content)
		if p.messages.IsAtBottom() {
			return p, tea.Batch(cmd, p.messages.ScrollToBottom())
		}
		return p, cmd
	case *runtime.SessionTitleEvent:
		p.sessionTitle = msg.Title
	case *runtime.TokenUsageEvent:
		p.sidebar.SetTokenUsage(msg.Usage)
	case *runtime.StreamStoppedEvent:
		spinnerCmd := p.setWorking(false)
		cmd := p.messages.AddSeparatorMessage()
		if p.msgCancel != nil {
			p.msgCancel = nil
		}
		p.stopProgressBar()
		return p, tea.Batch(cmd, p.messages.ScrollToBottom(), spinnerCmd)
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
			Model: dialog.NewToolConfirmationDialog(msg.ToolCall, p.app),
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
	// Header
	headerText := p.title
	header := styles.HeaderStyle.Render(headerText + " " + p.sessionTitle)

	// Main chat content area (without input)
	// Calculate chat width (85% of available width)
	innerWidth := p.width // subtract app style padding
	sidebarWidth := int(float64(innerWidth) * 0.15)
	chatWidth := innerWidth - sidebarWidth

	chatView := styles.ChatStyle.
		Height(p.chatHeight).
		Width(chatWidth).
		Render(p.messages.View())

	// Sidebar with explicit height constraint to prevent disappearing during scroll
	sidebarView := lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(p.chatHeight).
		Align(lipgloss.Left, lipgloss.Top).
		Render(p.sidebar.View())

	// Create horizontal layout with chat content and sidebar
	bodyContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		chatView,
		sidebarView,
	)

	// Input field spans full width below everything
	input := p.editor.View()

	// Create a full-height layout with header, body, and input
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		bodyContent,
		input,
	)

	return styles.AppStyle.
		Height(p.height).
		Render(content)
}

// SetSize sets the dimensions of the chat page
func (p *chatPage) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height

	var cmds []tea.Cmd

	// Calculate heights accounting for padding
	headerHeight := 3 // header + top/bottom padding
	editorHeight := 3 // fixed 3 lines for multi-line input

	// Calculate available space, ensuring status bar remains visible
	availableHeight := height - headerHeight
	p.inputHeight = editorHeight + 2 // account for editor padding
	p.chatHeight = availableHeight - p.inputHeight

	// Account for horizontal padding in width
	innerWidth := width - 2 // subtract left/right padding

	// Calculate sidebar and main content widths (15% sidebar, 85% main)
	sidebarWidth := int(float64(innerWidth) * 0.15)
	mainWidth := innerWidth - sidebarWidth

	// Set component sizes
	cmds = append(cmds,
		p.messages.SetSize(mainWidth, p.chatHeight),
		p.sidebar.SetSize(sidebarWidth, p.chatHeight),
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
	}

	// Add focused component bindings
	switch p.focusedPanel {
	case PanelChat:
		bindings = append(bindings, p.messages.Bindings()...)
	case PanelEditor:
		bindings = append(bindings, p.editor.Bindings()...)
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

// processMessage processes a message with the runtime
func (p *chatPage) processMessage(content string) tea.Cmd {
	if p.msgCancel != nil {
		p.msgCancel()
	}

	var ctx context.Context
	ctx, p.msgCancel = context.WithCancel(context.Background())

	if strings.HasPrefix(content, "!") {
		p.app.RunBangCommand(ctx, content[1:])
	} else {
		p.app.Run(ctx, p.msgCancel, content)
	}

	return p.messages.ScrollToBottom()
}

func (p *chatPage) CopySessionToClipboard() tea.Cmd {
	transcript := p.messages.PlainTextTranscript()
	if transcript == "" {
		cmd := p.messages.AddSystemMessage("Conversation is empty; nothing copied.")
		return tea.Batch(cmd, p.messages.ScrollToBottom())
	}

	if err := clipboard.WriteAll(transcript); err != nil {
		cmd := p.messages.AddSystemMessage("Failed to copy conversation: " + err.Error())
		return tea.Batch(cmd, p.messages.ScrollToBottom())
	}

	cmd := p.messages.AddSystemMessage("Conversation copied to clipboard.")
	return tea.Batch(cmd, p.messages.ScrollToBottom())
}

// CompactSession generates a summary and compacts the session history
func (p *chatPage) CompactSession() tea.Cmd {
	if p.msgCancel != nil {
		p.msgCancel()
		p.msgCancel = nil
	}

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
