package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/styles"
)

// maxIterationsDialog implements DialogModel for max iterations confirmation
type maxIterationsDialog struct {
	width, height int
	maxIterations int
	app           *app.App
	keyMap        maxIterationsKeyMap
}

// SetSize implements Dialog.
func (d *maxIterationsDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}

// maxIterationsKeyMap defines key bindings for max iterations confirmation dialog
type maxIterationsKeyMap struct {
	Yes key.Binding
	No  key.Binding
}

// defaultMaxIterationsKeyMap returns default key bindings
func defaultMaxIterationsKeyMap() maxIterationsKeyMap {
	return maxIterationsKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "continue"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("N", "stop"),
		),
	}
}

// NewMaxIterationsDialog creates a new max iterations confirmation dialog
func NewMaxIterationsDialog(maxIterations int, appInstance *app.App) Dialog {
	return &maxIterationsDialog{
		maxIterations: maxIterations,
		app:           appInstance,
		keyMap:        defaultMaxIterationsKeyMap(),
	}
}

// Init initializes the max iterations confirmation dialog
func (d *maxIterationsDialog) Init() tea.Cmd {
	return nil
}

// Update handles messages for the max iterations confirmation dialog
func (d *maxIterationsDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Yes):
			if d.app != nil {
				d.app.Resume("approve")
			}
			return d, core.CmdHandler(CloseDialogMsg{})
		case key.Matches(msg, d.keyMap.No):
			if d.app != nil {
				d.app.Resume("reject")
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
func (d *maxIterationsDialog) Position() (row, col int) {
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

// View renders the max iterations confirmation dialog
func (d *maxIterationsDialog) View() string {
	// clamped width: ~60% of screen, bounded by [36, 84] and screen margin
	dialogWidth := d.width * 60 / 100
	if dialogWidth < 36 {
		dialogWidth = max(20, min(d.width-4, 36))
	}
	if dialogWidth > 84 {
		dialogWidth = min(84, d.width-4)
	}

	padX := 2
	padY := 1

	// Border takes one character on each side when present
	frameHorizontal := (padX * 2) + 2
	contentWidth := max(10, dialogWidth-frameHorizontal)

	dialogStyle := styles.DialogWarningStyle.
		Padding(padY, padX).
		Width(dialogWidth)

	title := styles.DialogTitleWarningStyle.
		Width(contentWidth).
		Render("Maximum Iterations Reached")

	separatorWidth := max(1, contentWidth)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))

	// Info section
	infoText := fmt.Sprintf("Max Iterations: %d", d.maxIterations)
	infoWrapped := wrapDisplayText(infoText, contentWidth)
	infoSection := styles.DialogContentStyle.Render(infoWrapped)

	// Message section
	message := styles.DialogContentStyle.Render(wrapDisplayText("The agent may be stuck in a loop. This can happen with smaller or less capable models.", contentWidth))

	// Question section
	question := styles.DialogQuestionStyle.
		Width(contentWidth).
		Render(wrapDisplayText("Do you want to continue for 10 more iterations?", contentWidth))

	// Options section
	options := styles.DialogOptionsStyle.
		Width(contentWidth).
		Render(wrapDisplayText("[Y]es    [N]o", contentWidth))

	// Combine all parts with proper spacing
	parts := []string{title, separator, infoSection, "", message, "", question, "", options}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return dialogStyle.Render(content)
}

// width-aware wrapping based on display cell width
func wrapDisplayText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	var lines []string
	var current string
	for _, w := range words {
		if lipgloss.Width(current) == 0 {
			current = w
			continue
		}
		if lipgloss.Width(current+" "+w) <= maxWidth {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}
