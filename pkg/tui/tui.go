package tui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/browser"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/runtime"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

// appModel represents the main application model
type appModel struct {
	application     *app.App
	wWidth, wHeight int // Window dimensions
	width, height   int
	keyMap          KeyMap

	chatPage  chat.Page
	statusBar statusbar.StatusBar

	notification notification.Manager
	dialog       dialog.Manager
	completions  completion.Manager

	// Session state
	sessionState *service.SessionState

	// State
	ready bool
	err   error
}

// KeyMap defines global key bindings
type KeyMap struct {
	Quit           key.Binding
	CommandPalette key.Binding
}

// DefaultKeyMap returns the default global key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		CommandPalette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
	}
}

// New creates and initializes a new TUI application model
func New(ctx context.Context, a *app.App) tea.Model {
	sessionState := service.NewSessionState()

	t := &appModel{
		keyMap:       DefaultKeyMap(),
		dialog:       dialog.New(),
		notification: notification.New(),
		completions:  completion.New(),
		application:  a,
		sessionState: sessionState,
	}

	t.statusBar = statusbar.New(t)
	t.chatPage = chat.New(a, sessionState)

	// Make sure to stop the progress bar when the app quits abruptly.
	go func() {
		<-ctx.Done()
		t.chatPage.Cleanup()
	}()

	return t
}

// Init initializes the application
func (a *appModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.dialog.Init(),
		a.chatPage.Init(),
		a.emitStartupInfo(),
	}

	if firstMessage := a.application.FirstMessage(); firstMessage != nil {
		cmds = append(cmds, func() tea.Msg {
			return editor.SendMsg{
				Content: a.application.ResolveCommand(context.Background(), *firstMessage),
			}
		})
	}

	return tea.Batch(cmds...)
}

// emitStartupInfo creates a command that emits startup events for immediate sidebar display
func (a *appModel) emitStartupInfo() tea.Cmd {
	return func() tea.Msg {
		// a buffered channel to collect startup events
		events := make(chan runtime.Event, 10)

		go func() {
			defer close(events)
			a.application.EmitStartupInfo(context.Background(), events)
		}()

		var collectedEvents []runtime.Event
		for event := range events {
			collectedEvents = append(collectedEvents, event)
		}

		return StartupEventsMsg{Events: collectedEvents}
	}
}

// StartupEventsMsg carries startup events to be processed by the UI
type StartupEventsMsg struct {
	Events []runtime.Event
}

// Help returns help information
func (a *appModel) Help() help.KeyMap {
	return core.NewSimpleHelp(a.Bindings())
}

func (a *appModel) Bindings() []key.Binding {
	return append([]key.Binding{
		a.keyMap.Quit,
		a.keyMap.CommandPalette,
	}, a.chatPage.Bindings()...)
}

// Update handles incoming messages and updates the application state
func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// Handle dialog-specific messages first
	case dialog.OpenDialogMsg, dialog.CloseDialogMsg:
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd

	case StartupEventsMsg:
		var cmds []tea.Cmd
		for _, event := range msg.Events {
			updated, cmd := a.chatPage.Update(event)
			a.chatPage = updated.(chat.Page)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return a, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		a.wWidth, a.wHeight = msg.Width, msg.Height
		cmd := a.handleWindowResize(msg.Width, msg.Height)
		a.completions.Update(msg)
		return a, cmd

	case notification.ShowMsg, notification.HideMsg:
		updated, cmd := a.notification.Update(msg)
		a.notification = updated
		return a, cmd

	case tea.KeyPressMsg:
		cmd := a.handleKeyPressMsg(msg)
		return a, cmd

	case tea.MouseWheelMsg:
		// If dialogs are active, they get priority for mouse events
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}
		// Otherwise forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.NewSessionMsg:
		a.application.NewSession()
		a.sessionState = service.NewSessionState()
		a.chatPage = chat.New(a.application, a.sessionState)
		a.dialog = dialog.New()
		a.statusBar = statusbar.New(a.chatPage)

		return a, tea.Batch(a.Init(), a.handleWindowResize(a.wWidth, a.wHeight))

	case messages.EvalSessionMsg:
		evalFile, _ := evaluation.Save(a.application.Session())
		return a, core.CmdHandler(notification.ShowMsg{Text: fmt.Sprintf("Eval saved to file %s", evalFile)})

	case messages.CompactSessionMsg:
		return a, a.chatPage.CompactSession()

	case messages.CopySessionToClipboardMsg:
		transcript := a.application.PlainTextTranscript()
		if transcript == "" {
			return a, core.CmdHandler(notification.ShowMsg{Text: "Conversation is empty; nothing copied."})
		}

		if err := clipboard.WriteAll(transcript); err != nil {
			return a, core.CmdHandler(notification.ShowMsg{Text: "Failed to copy conversation: " + err.Error(), Type: notification.TypeError})
		}

		return a, core.CmdHandler(notification.ShowMsg{Text: "Conversation copied to clipboard."})

	case messages.ToggleYoloMsg:
		sess := a.application.Session()
		sess.ToolsApproved = !sess.ToolsApproved
		var statusText string
		if sess.ToolsApproved {
			statusText = "Yolo mode enabled: tools will be auto-approved"
		} else {
			statusText = "Yolo mode disabled: tools will require confirmation"
		}
		return a, core.CmdHandler(notification.ShowMsg{Text: statusText})

	case messages.AgentCommandMsg:
		resolvedCommand := a.application.ResolveCommand(context.Background(), msg.Command)
		return a, core.CmdHandler(editor.SendMsg{Content: resolvedCommand})

	case messages.ShowMCPPromptInputMsg:
		// Convert the interface{} back to mcptools.PromptInfo
		promptInfo, ok := msg.PromptInfo.(mcptools.PromptInfo)
		if !ok {
			return a, core.CmdHandler(notification.ShowMsg{Text: "Invalid prompt info"})
		}
		// Show the MCP prompt input dialog
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewMCPPromptInputDialog(msg.PromptName, promptInfo),
		})

	case messages.MCPPromptMsg:
		// Execute MCP prompt and send the result as editor content
		promptContent, err := a.application.ExecuteMCPPrompt(context.Background(), msg.PromptName, msg.Arguments)
		if err != nil {
			errorMsg := fmt.Sprintf("Error executing MCP prompt '%s': %v", msg.PromptName, err)
			return a, core.CmdHandler(notification.ShowMsg{Text: errorMsg})
		}
		return a, core.CmdHandler(editor.SendMsg{Content: promptContent})

	case messages.OpenURLMsg:
		_ = browser.Open(context.Background(), msg.URL)
		return a, nil

	case dialog.RuntimeResumeMsg:
		a.application.Resume(msg.Response)
		return a, nil

	case error:
		a.err = msg
		return a, nil

	default:
		if _, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			// Always forward runtime events to chat page
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			return a, cmd
		}

		// For other messages, check if dialogs should handle them first
		// If dialogs are active, they get priority for input
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}

		var cmds []tea.Cmd
		var cmd tea.Cmd

		updated, cmd := a.completions.Update(msg)
		cmds = append(cmds, cmd)
		a.completions = updated.(completion.Manager)

		updated, cmd = a.chatPage.Update(msg)
		cmds = append(cmds, cmd)
		a.chatPage = updated.(chat.Page)

		return a, tea.Batch(cmds...)
	}
}

// handleWindowResize processes window resize events
func (a *appModel) handleWindowResize(width, height int) tea.Cmd {
	var cmds []tea.Cmd

	// Update dimensions
	a.width, a.height = width, height-1 // Account for status bar

	if !a.ready {
		a.ready = true
	}

	// Update dialog system
	u, cmd := a.dialog.Update(tea.WindowSizeMsg{Width: width, Height: height})
	a.dialog = u.(dialog.Manager)
	cmds = append(cmds, cmd)

	cmd = a.chatPage.SetSize(a.width, a.height)
	cmds = append(cmds, cmd)

	// Update status bar width
	a.statusBar.SetWidth(a.width)

	// Update notification size
	a.notification.SetSize(a.width, a.height)

	return tea.Batch(cmds...)
}

func (a *appModel) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return dialogCmd
	}

	if a.completions.Open() {
		// Check if this is a navigation key that the completion manager should handle
		switch msg.String() {
		case "up", "down", "enter", "esc":
			// Let completion manager handle navigation keys
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			return completionCmd
		default:
			// For all other keys (typing), send to both completion (for filtering) and editor
			var cmds []tea.Cmd
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			cmds = append(cmds, completionCmd)

			// Also send to chat page/editor so user can continue typing
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			cmds = append(cmds, cmd)

			return tea.Batch(cmds...)
		}
	}

	switch {
	case key.Matches(msg, a.keyMap.Quit):
		a.chatPage.Cleanup()
		return tea.Quit
	case key.Matches(msg, a.keyMap.CommandPalette):
		categories := commands.BuildCommandCategories(context.Background(), a.application)
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewCommandPaletteDialog(categories),
		})
	default:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return cmd
	}
}

// View renders the complete application interface
func (a *appModel) View() tea.View {
	// Show error if present
	if a.err != nil {
		return toFullscreenView(styles.ErrorStyle.Render(a.err.Error()))
	}

	// Show loading if not ready
	if !a.ready {
		return toFullscreenView(
			styles.CenterStyle.
				Width(a.wWidth).
				Height(a.wHeight).
				Render(styles.MutedStyle.Render("Loading...")),
		)
	}

	// Render chat page
	pageView := a.chatPage.View()

	// Create status bar
	statusBar := a.statusBar.View()

	// Combine page view with status bar
	var components []string
	components = append(components, pageView)
	if statusBar != "" {
		components = append(components, statusBar)
	}

	baseView := lipgloss.JoinVertical(lipgloss.Top, components...)

	hasOverlays := a.dialog.Open() || a.notification.Open() || a.completions.Open()

	if hasOverlays {
		baseLayer := lipgloss.NewLayer(baseView)
		var allLayers []*lipgloss.Layer
		allLayers = append(allLayers, baseLayer)

		// Add dialog layers
		if a.dialog.Open() {
			dialogLayers := a.dialog.GetLayers()
			allLayers = append(allLayers, dialogLayers...)
		}

		if a.notification.Open() {
			allLayers = append(allLayers, a.notification.GetLayer())
		}

		if a.completions.Open() {
			layers := a.completions.GetLayers()
			allLayers = append(allLayers, layers...)
		}

		canvas := lipgloss.NewCanvas(allLayers...)
		return toFullscreenView(canvas.Render())
	}

	return toFullscreenView(baseView)
}

func toFullscreenView(content string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background

	return view
}
