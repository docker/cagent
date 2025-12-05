package dialog

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// Dialog sizing constants
const (
	dialogSizePercent  = 70 // dialog uses 70% of terminal dimensions
	dialogFramePadding = 6  // border (2) + internal padding (4)
	dialogMinWidth     = 20
	dialogChromeRows   = 4 // title + separator + blank line + help
	dialogFrameHeight  = 4 // top/bottom border + padding
	scrollPadding      = 2 // blank lines at end so user can scroll content up
	minViewportHeight  = 5
)

type attachmentPreviewDialog struct {
	width, height int
	preview       editor.AttachmentPreview
	viewport      viewport.Model

	titleView     string
	separatorView string
	helpView      string
	dialogWidth   int
	dialogHeight  int
	innerWidth    int
}

// NewAttachmentPreviewDialog returns a dialog that shows attachment content in a scrollable view.
func NewAttachmentPreviewDialog(preview editor.AttachmentPreview) Dialog {
	vp := viewport.New(
		viewport.WithWidth(80),
		viewport.WithHeight(20),
	)
	vp.SoftWrap = false
	vp.SetContent(preview.Content)

	return &attachmentPreviewDialog{
		preview:  preview,
		viewport: vp,
	}
}

func (d *attachmentPreviewDialog) Init() tea.Cmd {
	return nil
}

func (d *attachmentPreviewDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return d, core.CmdHandler(CloseDialogMsg{})
		}
	}

	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return d, cmd
}

func (d *attachmentPreviewDialog) View() string {
	// Constrain viewport output to fixed dimensions to prevent layout shifts
	viewportView := lipgloss.NewStyle().
		Width(d.innerWidth).
		MaxWidth(d.innerWidth).
		Height(d.viewport.Height()).
		MaxHeight(d.viewport.Height()).
		Render(d.viewport.View())

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		d.titleView,
		d.separatorView,
		viewportView,
		"",
		d.helpView,
	)

	return styles.DialogStyle.
		Width(d.dialogWidth).
		Height(d.dialogHeight).
		Render(content)
}

func (d *attachmentPreviewDialog) Position() (row, col int) {
	// Use pre-computed dimensions for stable positioning
	dialogHeight := d.dialogHeight
	if dialogHeight == 0 {
		dialogHeight = 20 // fallback before SetSize is called
	}
	dialogWidth := d.dialogWidth
	if dialogWidth == 0 {
		dialogWidth = dialogMinWidth
	}

	row = max(0, (d.height-dialogHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}

func (d *attachmentPreviewDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height

	// Cache computed dimensions
	d.dialogWidth = d.computeDialogWidth()
	d.innerWidth = max(dialogMinWidth, d.dialogWidth-dialogFramePadding)

	maxDialogHeight := max(10, (height*dialogSizePercent)/100)
	chromeHeight := dialogChromeRows + dialogFrameHeight
	viewportHeight := max(minViewportHeight, maxDialogHeight-chromeHeight)
	d.dialogHeight = chromeHeight + viewportHeight

	// Pre-render chrome elements
	d.titleView = renderSingleLine(styles.DialogTitleInfoStyle, d.preview.Title, d.innerWidth)
	d.separatorView = styles.DialogSeparatorStyle.
		Width(d.innerWidth).
		Align(lipgloss.Center).
		Render(strings.Repeat("─", d.innerWidth))

	helpText := "[esc/q] close | scroll: ↑↓ / wheel | pan: ←→"
	d.helpView = renderSingleLine(styles.DialogHelpStyle, helpText, d.innerWidth)

	d.viewport.SetWidth(d.innerWidth)
	d.viewport.SetHeight(viewportHeight)

	// Add blank lines so user can scroll until only 1 line of content remains visible
	paddedContent := d.preview.Content + strings.Repeat("\n", viewportHeight-scrollPadding)
	d.viewport.SetContent(paddedContent)

	return nil
}

func (d *attachmentPreviewDialog) computeDialogWidth() int {
	width := d.width * dialogSizePercent / 100
	if width < 40 {
		width = d.width - 4
	}
	return max(dialogMinWidth, width)
}

func renderSingleLine(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return ""
	}
	trimmed := ansi.Truncate(text, width, "…")
	padded := trimmed + strings.Repeat(" ", max(0, width-lipgloss.Width(trimmed)))
	return style.Width(width).Render(padded)
}
