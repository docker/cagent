package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// URLElicitationDialog handles URL-based MCP elicitation requests.
// It displays a URL for the user to visit and waits for confirmation.
type URLElicitationDialog struct {
	BaseDialog
	message string
	url     string
	keyMap  ConfirmKeyMap
	escape  key.Binding
}

// NewURLElicitationDialog creates a new URL elicitation dialog.
func NewURLElicitationDialog(message, url string) Dialog {
	return &URLElicitationDialog{
		message: message,
		url:     url,
		keyMap:  DefaultConfirmKeyMap(),
		escape:  key.NewBinding(key.WithKeys("esc")),
	}
}

func (d *URLElicitationDialog) Init() tea.Cmd {
	return nil
}

func (d *URLElicitationDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd
	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}
		switch {
		case key.Matches(msg, d.keyMap.Yes):
			cmd := d.respond(tools.ElicitationActionAccept)
			return d, cmd
		case key.Matches(msg, d.keyMap.No):
			cmd := d.respond(tools.ElicitationActionDecline)
			return d, cmd
		case key.Matches(msg, d.escape):
			cmd := d.respond(tools.ElicitationActionCancel)
			return d, cmd
		}
	}
	return d, nil
}

func (d *URLElicitationDialog) respond(action tools.ElicitationAction) tea.Cmd {
	return CloseWithElicitationResponse(action, nil)
}

func (d *URLElicitationDialog) Position() (row, col int) {
	return d.CenterDialog(d.View())
}

func (d *URLElicitationDialog) View() string {
	dialogWidth := d.ComputeDialogWidth(70, 50, 90)
	contentWidth := d.ContentWidth(dialogWidth, 2)

	content := NewContent(contentWidth)
	content.AddTitle("MCP Server Request")
	content.AddSeparator()

	// Message from server
	content.AddContent(styles.DialogContentStyle.Width(contentWidth).Render(d.message))
	content.AddSpace()

	// URL to visit
	if d.url != "" {
		content.AddContent(styles.DialogContentStyle.Foreground(styles.TextMuted).Render("Please visit:"))
		content.AddContent(styles.InfoStyle.Width(contentWidth).Render(d.url))
		content.AddSpace()
	}

	content.AddHelp("Press Y when you have completed the action, or N to decline.")
	content.AddSpace()
	content.AddHelpKeys("Y", "confirm", "N", "decline", "esc", "cancel")

	return styles.DialogStyle.Width(dialogWidth).Render(content.Build())
}
