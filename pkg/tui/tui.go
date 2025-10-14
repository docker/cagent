package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	chatpage "github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/styles"
)

var lastMouseEvent time.Time

// MouseEventFilter filters mouse events to prevent spam
func MouseEventFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case tea.MouseWheelMsg, tea.MouseMotionMsg:
		now := time.Now()
		if now.Sub(lastMouseEvent) < 20*time.Millisecond {
			return nil
		}
		lastMouseEvent = now
	}
	return msg
}

// appModel represents the main application model
type appModel struct {
	application     *app.App
	wWidth, wHeight int // Window dimensions
	width, height   int
	keyMap          KeyMap

	chatPage  chatpage.Page
	statusBar statusbar.StatusBar

	// Dialog system
	dialog dialog.Manager

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
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		CommandPalette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "command palette"),
		),
	}
}

// New creates and initializes a new TUI application model
func New(a *app.App) tea.Model {
	t := &appModel{
		chatPage:    chatpage.New(a),
		keyMap:      DefaultKeyMap(),
		dialog:      dialog.New(),
		application: a,
	}

	t.statusBar = statusbar.New(t)

	return t
}

// Init initializes the application
func (a *appModel) Init() tea.Cmd {
	return tea.Batch(
		a.dialog.Init(),
		a.chatPage.Init(),
	)
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

	case tea.WindowSizeMsg:
		a.wWidth, a.wHeight = msg.Width, msg.Height
		cmd := a.handleWindowResize(msg.Width, msg.Height)
		return a, cmd

	case tea.KeyPressMsg:
		cmd := a.handleKeyPressMsg(msg)
		return a, cmd

	case tea.MouseWheelMsg:
		// If dialogs are active, they get priority for mouse events
		if a.dialog.HasDialog() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}
		// Otherwise forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chatpage.Page)
		return a, cmd

	case error:
		a.err = msg
		return a, nil

	default:
		if _, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			// Always forward runtime events to chat page
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chatpage.Page)
			return a, cmd
		}

		// For other messages, check if dialogs should handle them first
		// If dialogs are active, they get priority for input
		if a.dialog.HasDialog() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}

		// Otherwise, forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chatpage.Page)
		return a, cmd
	}
}

// handleWindowResize processes window resize events
func (a *appModel) handleWindowResize(width, height int) tea.Cmd {
	var cmds []tea.Cmd

	// Update dimensions
	a.width, a.height = width, height-2 // Account for status bar

	if !a.ready {
		a.ready = true
	}

	// Update dialog system
	u, cmd := a.dialog.Update(tea.WindowSizeMsg{Width: width, Height: height})
	a.dialog = u.(dialog.Manager)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Update chat page
	if sizable, ok := a.chatPage.(interface{ SetSize(int, int) tea.Cmd }); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	} else {
		// Fallback: send window size message
		updated, cmd := a.chatPage.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		a.chatPage = updated.(chatpage.Page)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Update status bar width
	a.statusBar.SetWidth(a.width)

	return tea.Batch(cmds...)
}

// handleKeyPressMsg processes keyboard input
func (a *appModel) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	// If dialogs are active, they handle key input first
	if a.dialog.HasDialog() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return dialogCmd
	}

	switch {
	case key.Matches(msg, a.keyMap.Quit):
		a.chatPage.Cleanup()
		return tea.Quit
	case key.Matches(msg, a.keyMap.CommandPalette):
		// Open command palette
		categories := a.buildCommandCategories()
		return dialog.OpenCommandPalette(categories)
	default:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chatpage.Page)
		return cmd
	}
}

// View renders the complete application interface
func (a *appModel) View() tea.View {
	// Handle minimum window size
	if a.wWidth < 25 || a.wHeight < 15 {
		return toFullscreenView(styles.CenterStyle.
			Width(a.wWidth).
			Height(a.wHeight).
			Render(
				styles.BorderStyle.
					Padding(1, 1).
					Foreground(lipgloss.Color("#ffffff")).
					BorderForeground(lipgloss.Color("#ff5f87")).
					Render("Window too small!"),
			),
		)
	}

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

	if a.dialog.HasDialog() {
		baseLayer := lipgloss.NewLayer(baseView)
		dialogLayers := a.dialog.GetLayers()

		allLayers := []*lipgloss.Layer{baseLayer}
		allLayers = append(allLayers, dialogLayers...)

		canvas := lipgloss.NewCanvas(allLayers...)
		return toFullscreenView(canvas.Render())
	}

	return toFullscreenView(baseView)
}

func toFullscreenView(content string) tea.View {
	view := tea.NewView(content)
	view.BackgroundColor = styles.Background
	return view
}

// buildCommandCategories builds the list of command categories for the command palette
func (a *appModel) buildCommandCategories() []dialog.CommandCategory {
	return []dialog.CommandCategory{
		{
			Name: "Session",
			Commands: []dialog.Command{
				{
					ID:          "session.new",
					Label:       "New ",
					Description: "Start a new conversation",
					Category:    "Session",
					Execute: func() tea.Cmd {
						a.application.NewSession()
						a.chatPage = chatpage.New(a.application)
						a.dialog = dialog.New()
						a.statusBar = statusbar.New(a.chatPage)

						return tea.Batch(a.Init(), a.handleWindowResize(a.wWidth, a.wHeight))
					},
				},
				{
					ID:          "session.compact",
					Label:       "Compact",
					Description: "Summarize the current conversation",
					Category:    "Session",
					Execute: func() tea.Cmd {
						return a.chatPage.CompactSession()
					},
				},
				{
					ID:          "session.clipboard",
					Label:       "Copy",
					Description: "Copy the current conversation to the clipboard",
					Category:    "Session",
					Execute: func() tea.Cmd {
						return a.chatPage.CopySessionToClipboard()
					},
				},
			},
		},
	}
}
