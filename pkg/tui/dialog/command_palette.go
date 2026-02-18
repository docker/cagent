package dialog

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/scrollview"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// CommandExecuteMsg is sent when a command is selected
type CommandExecuteMsg struct {
	Command commands.Item
}

// commandPaletteDialog implements Dialog for the command palette
type commandPaletteDialog struct {
	BaseDialog
	textInput      textinput.Model
	categories     []commands.Category
	filtered       []commands.Item
	selected       int
	keyMap         commandPaletteKeyMap
	scrollview     *scrollview.Model
	lastClickTime  time.Time
	lastClickIndex int
}

// commandPaletteKeyMap defines key bindings for the command palette
type commandPaletteKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
}

// defaultCommandPaletteKeyMap returns default key bindings
func defaultCommandPaletteKeyMap() commandPaletteKeyMap {
	return commandPaletteKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "ctrl+k"),
			key.WithHelp("↑/ctrl+k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "ctrl+j"),
			key.WithHelp("↓/ctrl+j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "execute"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
	}
}

// NewCommandPaletteDialog creates a new command palette dialog
func NewCommandPaletteDialog(categories []commands.Category) Dialog {
	ti := textinput.New()
	ti.SetStyles(styles.DialogInputStyle)
	ti.Placeholder = "Type to search commands…"
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)

	var allCommands []commands.Item
	for _, cat := range categories {
		allCommands = append(allCommands, cat.Commands...)
	}

	return &commandPaletteDialog{
		textInput:  ti,
		categories: categories,
		filtered:   allCommands,
		selected:   0,
		keyMap:     defaultCommandPaletteKeyMap(),
		scrollview: scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
	}
}

// Init initializes the command palette dialog
func (d *commandPaletteDialog) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the command palette dialog
func (d *commandPaletteDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	// Scrollview handles mouse scrollbar, wheel, and pgup/pgdn/home/end
	if handled, cmd := d.scrollview.Update(msg); handled {
		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		d.filterCommands()
		return d, cmd

	case tea.MouseClickMsg:
		// Scrollbar clicks already handled above; this handles list item clicks
		if msg.Button == tea.MouseLeft {
			if cmdIdx := d.mouseYToCommandIndex(msg.Y); cmdIdx >= 0 {
				now := time.Now()
				if cmdIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
					d.selected = cmdIdx
					d.lastClickTime = time.Time{}
					cmd := d.executeSelected()
					return d, cmd
				}
				d.selected = cmdIdx
				d.lastClickTime = now
				d.lastClickIndex = cmdIdx
			}
		}
		return d, nil

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
				d.scrollview.EnsureLineVisible(d.findSelectedLine())
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
				d.scrollview.EnsureLineVisible(d.findSelectedLine())
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.executeSelected()
			return d, cmd

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterCommands()
			return d, cmd
		}
	}

	return d, nil
}

// executeSelected executes the currently selected command and closes the dialog.
func (d *commandPaletteDialog) executeSelected() tea.Cmd {
	if d.selected < 0 || d.selected >= len(d.filtered) {
		return nil
	}
	selectedCmd := d.filtered[d.selected]
	cmds := []tea.Cmd{core.CmdHandler(CloseDialogMsg{})}
	if selectedCmd.Execute != nil {
		cmds = append(cmds, selectedCmd.Execute(""))
	}
	return tea.Sequence(cmds...)
}

// filterCommands filters the command list based on search input
func (d *commandPaletteDialog) filterCommands() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))

	if query == "" {
		d.filtered = make([]commands.Item, 0)
		for _, cat := range d.categories {
			d.filtered = append(d.filtered, cat.Commands...)
		}
		d.selected = 0
		d.scrollview.SetScrollOffset(0)
		return
	}

	d.filtered = make([]commands.Item, 0)
	for _, cat := range d.categories {
		for _, cmd := range cat.Commands {
			if strings.Contains(strings.ToLower(cmd.Label), query) ||
				strings.Contains(strings.ToLower(cmd.Description), query) ||
				strings.Contains(strings.ToLower(cmd.Category), query) ||
				strings.Contains(strings.ToLower(cmd.SlashCommand), query) {
				d.filtered = append(d.filtered, cmd)
			}
		}
	}

	if d.selected >= len(d.filtered) {
		d.selected = 0
	}
	d.scrollview.SetScrollOffset(0)
}

// Command palette dialog dimension constants
const (
	paletteWidthPercent  = 80
	paletteMinWidth      = 50
	paletteMaxWidth      = 80
	paletteHeightPercent = 70
	paletteMaxHeight     = 30
	paletteDialogPadding = 6 // horizontal padding inside dialog border
	paletteListOverhead  = 8 // title(1) + space(1) + input(1) + separator(1) + space(1) + help(1) + borders(2)
	paletteListStartY    = 6 // border(1) + padding(1) + title(1) + space(1) + input(1) + separator(1)
)

// dialogSize returns the dialog dimensions.
func (d *commandPaletteDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*paletteWidthPercent/100, paletteMaxWidth), paletteMinWidth)
	maxHeight = min(d.Height()*paletteHeightPercent/100, paletteMaxHeight)
	contentWidth = dialogWidth - paletteDialogPadding - d.scrollview.ReservedCols()
	return dialogWidth, maxHeight, contentWidth
}

// SetSize sets the dialog dimensions and configures the scrollview.
func (d *commandPaletteDialog) SetSize(width, height int) tea.Cmd {
	cmd := d.BaseDialog.SetSize(width, height)
	_, maxHeight, contentWidth := d.dialogSize()
	regionWidth := contentWidth + d.scrollview.ReservedCols()
	visLines := max(1, maxHeight-paletteListOverhead)
	d.scrollview.SetSize(regionWidth, visLines)
	return cmd
}

// mouseYToCommandIndex converts a mouse Y position to a command index.
// Returns -1 if the position is not on a command.
func (d *commandPaletteDialog) mouseYToCommandIndex(y int) int {
	dialogRow, _ := d.Position()
	visLines := d.scrollview.VisibleHeight()
	listStartY := dialogRow + paletteListStartY

	if y < listStartY || y >= listStartY+visLines {
		return -1
	}
	lineInView := y - listStartY
	actualLine := d.scrollview.ScrollOffset() + lineInView

	_, lineToCmd := d.buildLines(0)
	if actualLine < 0 || actualLine >= len(lineToCmd) {
		return -1
	}
	return lineToCmd[actualLine]
}

// buildLines builds the visual lines for the command list and returns:
// - lines: the rendered line strings
// - lineToCmd: maps each line index to command index (-1 for headers/blanks)
func (d *commandPaletteDialog) buildLines(contentWidth int) (lines []string, lineToCmd []int) {
	var lastCategory string
	for i, cmd := range d.filtered {
		if cmd.Category != lastCategory {
			if lastCategory != "" {
				lines = append(lines, "")
				lineToCmd = append(lineToCmd, -1)
			}
			if contentWidth > 0 {
				lines = append(lines, styles.PaletteCategoryStyle.MarginTop(0).Render(cmd.Category))
			} else {
				lines = append(lines, cmd.Category)
			}
			lineToCmd = append(lineToCmd, -1)
			lastCategory = cmd.Category
		}
		if contentWidth > 0 {
			lines = append(lines, d.renderCommand(cmd, i == d.selected, contentWidth))
		} else {
			lines = append(lines, "")
		}
		lineToCmd = append(lineToCmd, i)
	}
	return lines, lineToCmd
}

// findSelectedLine returns the line index that corresponds to the selected command.
func (d *commandPaletteDialog) findSelectedLine() int {
	_, lineToCmd := d.buildLines(0)
	for i, cmdIdx := range lineToCmd {
		if cmdIdx == d.selected {
			return i
		}
	}
	return 0
}

// View renders the command palette dialog
func (d *commandPaletteDialog) View() string {
	dialogWidth, _, contentWidth := d.dialogSize()
	d.textInput.SetWidth(contentWidth)

	allLines, _ := d.buildLines(contentWidth)
	regionWidth := contentWidth + d.scrollview.ReservedCols()

	// Set scrollview position for mouse hit-testing (auto-computed from dialog position)
	dialogRow, dialogCol := d.Position()
	d.scrollview.SetPosition(dialogCol+3, dialogRow+paletteListStartY)

	d.scrollview.SetContent(allLines, len(allLines))

	var scrollableContent string
	if len(d.filtered) == 0 {
		visLines := d.scrollview.VisibleHeight()
		emptyLines := []string{"", styles.DialogContentStyle.
			Italic(true).Align(lipgloss.Center).Width(contentWidth).
			Render("No commands found")}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		scrollableContent = d.scrollview.ViewWithLines(emptyLines)
	} else {
		scrollableContent = d.scrollview.View()
	}

	content := NewContent(regionWidth).
		AddTitle("Commands").
		AddSpace().
		AddContent(d.textInput.View()).
		AddSeparator().
		AddContent(scrollableContent).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "execute", "esc", "close").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

// renderCommand renders a single command in the list
func (d *commandPaletteDialog) renderCommand(cmd commands.Item, selected bool, contentWidth int) string {
	actionStyle := styles.PaletteUnselectedActionStyle
	descStyle := styles.PaletteUnselectedDescStyle
	if selected {
		actionStyle = styles.PaletteSelectedActionStyle
		descStyle = styles.PaletteSelectedDescStyle
	}

	label := " " + cmd.Label
	labelWidth := lipgloss.Width(actionStyle.Render(label))

	var content string
	content += actionStyle.Render(label)
	if cmd.Description != "" {
		separator := " • "
		separatorWidth := lipgloss.Width(separator)
		availableWidth := contentWidth - labelWidth - separatorWidth
		if availableWidth > 0 {
			truncatedDesc := toolcommon.TruncateText(cmd.Description, availableWidth)
			content += descStyle.Render(separator + truncatedDesc)
		}
	}
	return content
}

// Position calculates the position to center the dialog
func (d *commandPaletteDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
