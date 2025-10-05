package dialog

import (
	"fmt"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/tui/core"
)

// oauthAuthorizationDialog implements DialogModel for OAuth authorization confirmation
type oauthAuthorizationDialog struct {
	width, height int
	serverURL     string
	app           *app.App
	keyMap        oauthAuthorizationKeyMap
}

// SetSize implements Dialog.
func (d *oauthAuthorizationDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}

// oauthAuthorizationKeyMap defines key bindings for OAuth authorization confirmation dialog
type oauthAuthorizationKeyMap struct {
	Yes key.Binding
	No  key.Binding
}

// defaultOAuthAuthorizationKeyMap returns default key bindings
func defaultOAuthAuthorizationKeyMap() oauthAuthorizationKeyMap {
	return oauthAuthorizationKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "authorize"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("N", "decline"),
		),
	}
}

// NewOAuthAuthorizationDialog creates a new OAuth authorization confirmation dialog
func NewOAuthAuthorizationDialog(serverURL string, appInstance *app.App) Dialog {
	return &oauthAuthorizationDialog{
		serverURL: serverURL,
		app:       appInstance,
		keyMap:    defaultOAuthAuthorizationKeyMap(),
	}
}

// Init initializes the OAuth authorization confirmation dialog
func (d *oauthAuthorizationDialog) Init() tea.Cmd {
	return nil
}

// Update handles messages for the OAuth authorization confirmation dialog
func (d *oauthAuthorizationDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Yes):
			if d.app != nil {
				d.app.ResumeStartOAuth(true)
			}
			return d, core.CmdHandler(CloseDialogMsg{})
		case key.Matches(msg, d.keyMap.No):
			if d.app != nil {
				d.app.ResumeStartOAuth(false)
			}
			return d, core.CmdHandler(CloseDialogMsg{})
		}

		if msg.String() == "ctrl+c" {
			return d, tea.Quit
		}
	}

	return d, nil
}

// Position returns the dialog position (centered)
func (d *oauthAuthorizationDialog) Position() (row, col int) {
	// Render the dialog content to measure its actual dimensions
	dialogContent := d.View()

	// Get the actual rendered dimensions
	dialogWidth := lipgloss.Width(dialogContent)
	dialogHeight := lipgloss.Height(dialogContent)

	// Calculate centered position
	col = max(0, (d.width-dialogWidth)/2)
	row = max(0, (d.height-dialogHeight)/2)

	// Ensure dialog fits on screen
	if col+dialogWidth > d.width {
		col = max(0, d.width-dialogWidth)
	}
	if row+dialogHeight > d.height {
		row = max(0, d.height-dialogHeight)
	}

	return row, col
}

// View renders the OAuth authorization confirmation dialog
func (d *oauthAuthorizationDialog) View() string {
	// clamped width: ~60% of screen, bounded by [40, 90] and screen margin
	dialogWidth := d.width * 60 / 100
	if dialogWidth < 40 {
		dialogWidth = max(24, min(d.width-4, 40))
	}
	if dialogWidth > 90 {
		dialogWidth = min(90, d.width-4)
	}

	padX := 2
	padY := 1

	// Border takes one character on each side when present
	frameHorizontal := (padX * 2) + 2
	contentWidth := max(10, dialogWidth-frameHorizontal)

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f59e0b")).
		Foreground(lipgloss.Color("#d1d5db")).
		Padding(padY, padX).
		Width(dialogWidth).
		Align(lipgloss.Left)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3b82f6")).
		Align(lipgloss.Center).
		Width(contentWidth)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d1d5db")).
		Width(contentWidth)

	serverInfoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3b82f6")).
		Width(contentWidth)

	instructionsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b7280")).
		Width(contentWidth)

	optionsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10b981")).
		Align(lipgloss.Center).
		Width(contentWidth)

	// Content
	title := titleStyle.Render("🔐 OAuth Authorization Required")
	serverInfo := serverInfoStyle.Render(fmt.Sprintf("Server: %s (remote)", d.serverURL))
	description := messageStyle.Render("This server requires OAuth authentication to access its tools. Your browser will open automatically to complete the authorization process.")
	instructions := instructionsStyle.Render("After authorizing in your browser, return here and the agent will continue automatically.")
	options := optionsStyle.Render("Y - Authorize  |  N - Decline")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		serverInfo,
		"",
		description,
		"",
		instructions,
		"",
		options,
	)

	return dialogStyle.Render(content)
}
