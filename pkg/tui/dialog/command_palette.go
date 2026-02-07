package dialog

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
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
	textInput        textinput.Model
	categories       []commands.Category
	filtered         []commands.Item
	selected         int
	keyMap           commandPaletteKeyMap
	scrollbar        *scrollbar.Model
	needsScrollToSel bool // true when keyboard nav requires scrolling to selection
	lastClickTime    time.Time
	lastClickIndex   int
}

// commandPaletteKeyMap defines key bindings for the command palette
type commandPaletteKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Enter    key.Binding
	Escape   key.Binding
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
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdown", "page down"),
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

	// Build initial filtered list (all commands)
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
		scrollbar:  scrollbar.New(),
	}
}

// Init initializes the command palette dialog
func (d *commandPaletteDialog) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the command palette dialog
func (d *commandPaletteDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		// Forward paste to text input
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		cmds = append(cmds, cmd)
		d.filterCommands()
		return d, tea.Batch(cmds...)

	case tea.MouseClickMsg:
		return d.handleMouseClick(msg)

	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return d.handleMouseDrag(msg)

	case tea.MouseWheelMsg:
		return d.handleMouseWheel(msg)

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
				d.needsScrollToSel = true
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
				d.needsScrollToSel = true
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageUp):
			d.selected -= d.visibleLines()
			if d.selected < 0 {
				d.selected = 0
			}
			d.needsScrollToSel = true
			return d, nil

		case key.Matches(msg, d.keyMap.PageDown):
			d.selected += d.visibleLines()
			if d.selected >= len(d.filtered) {
				d.selected = max(0, len(d.filtered)-1)
			}
			d.needsScrollToSel = true
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.executeSelected()
			return d, cmd

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			cmds = append(cmds, cmd)
			d.filterCommands()
		}
	}

	return d, tea.Batch(cmds...)
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

// handleMouseClick handles mouse click events on the dialog
func (d *commandPaletteDialog) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	// Check if click is on the scrollbar
	if d.isMouseOnScrollbar(msg.X, msg.Y) {
		d.scrollbar, _ = d.scrollbar.Update(msg)
		return d, nil
	}

	// Check if click is on a command in the list
	if msg.Button != tea.MouseLeft {
		return d, nil
	}
	cmdIdx := d.mouseYToCommandIndex(msg.Y)
	if cmdIdx < 0 {
		return d, nil
	}

	now := time.Now()
	// Double-click: execute
	if cmdIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
		d.selected = cmdIdx
		d.lastClickTime = time.Time{}
		cmd := d.executeSelected()
		return d, cmd
	}
	// Single click: select
	d.selected = cmdIdx
	d.lastClickTime = now
	d.lastClickIndex = cmdIdx
	return d, nil
}

// handleMouseDrag handles mouse drag/release for scrollbar
func (d *commandPaletteDialog) handleMouseDrag(msg tea.Msg) (layout.Model, tea.Cmd) {
	if d.scrollbar.IsDragging() {
		d.scrollbar, _ = d.scrollbar.Update(msg)
	}
	return d, nil
}

// handleMouseWheel handles mouse wheel scrolling inside the dialog
func (d *commandPaletteDialog) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	if !d.isMouseInDialog(msg.X, msg.Y) {
		return d, nil
	}
	switch msg.Button.String() {
	case "wheelup":
		d.scrollbar.ScrollUp()
		d.scrollbar.ScrollUp()
	case "wheeldown":
		d.scrollbar.ScrollDown()
		d.scrollbar.ScrollDown()
	}
	return d, nil
}

// filterCommands filters the command list based on search input
func (d *commandPaletteDialog) filterCommands() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))

	if query == "" {
		// Show all commands
		d.filtered = make([]commands.Item, 0)
		for _, cat := range d.categories {
			d.filtered = append(d.filtered, cat.Commands...)
		}
		d.selected = 0
		d.scrollbar.SetScrollOffset(0)
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
	d.scrollbar.SetScrollOffset(0)
}

// Command palette dialog dimension constants
const (
	paletteWidthPercent    = 80
	paletteMinWidth        = 50
	paletteMaxWidth        = 80
	paletteHeightPercent   = 70
	paletteMaxHeight       = 30
	paletteDialogPadding   = 6 // horizontal padding inside dialog border
	paletteListOverhead    = 8 // title(1) + space(1) + input(1) + separator(1) + space(1) + help(1) + borders(2)
	paletteListStartY      = 6 // border(1) + padding(1) + title(1) + space(1) + input(1) + separator(1)
	paletteScrollbarXInset = 3
	paletteScrollbarGap    = 1
)

// dialogSize returns the dialog dimensions.
func (d *commandPaletteDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*paletteWidthPercent/100, paletteMaxWidth), paletteMinWidth)
	maxHeight = min(d.Height()*paletteHeightPercent/100, paletteMaxHeight)
	contentWidth = dialogWidth - paletteDialogPadding - scrollbar.Width - paletteScrollbarGap
	return dialogWidth, maxHeight, contentWidth
}

// visibleLines returns the number of lines available for the command list.
func (d *commandPaletteDialog) visibleLines() int {
	_, maxHeight, _ := d.dialogSize()
	return max(1, maxHeight-paletteListOverhead)
}

// isMouseInDialog checks if the mouse position is inside the dialog bounds
func (d *commandPaletteDialog) isMouseInDialog(x, y int) bool {
	dialogRow, dialogCol := d.Position()
	dialogWidth, maxHeight, _ := d.dialogSize()
	return x >= dialogCol && x < dialogCol+dialogWidth &&
		y >= dialogRow && y < dialogRow+maxHeight
}

// isMouseOnScrollbar checks if the mouse position is on the scrollbar
func (d *commandPaletteDialog) isMouseOnScrollbar(x, y int) bool {
	lines, lineToCmd := d.buildLines(0)
	if len(lines) <= d.visibleLines() {
		return false
	}
	_ = lineToCmd // used for other purposes
	dialogRow, dialogCol := d.Position()
	dialogWidth, _, _ := d.dialogSize()
	scrollbarX := dialogCol + dialogWidth - paletteScrollbarXInset - scrollbar.Width
	scrollbarY := dialogRow + paletteListStartY
	visLines := d.visibleLines()
	return x >= scrollbarX && x < scrollbarX+scrollbar.Width &&
		y >= scrollbarY && y < scrollbarY+visLines
}

// mouseYToCommandIndex converts a mouse Y position to a command index.
// Returns -1 if the position is not on a command.
func (d *commandPaletteDialog) mouseYToCommandIndex(y int) int {
	dialogRow, _ := d.Position()
	visLines := d.visibleLines()
	listStartY := dialogRow + paletteListStartY

	if y < listStartY || y >= listStartY+visLines {
		return -1
	}
	lineInView := y - listStartY
	actualLine := d.scrollbar.GetScrollOffset() + lineInView

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
	visLines := d.visibleLines()
	d.textInput.SetWidth(contentWidth)

	// Build all lines with command mapping
	allLines, _ := d.buildLines(contentWidth)
	totalLines := len(allLines)

	// Update scrollbar dimensions
	d.scrollbar.SetDimensions(visLines, totalLines)

	// Auto-scroll to selection when keyboard navigation occurred
	if d.needsScrollToSel {
		selectedLine := d.findSelectedLine()
		scrollOffset := d.scrollbar.GetScrollOffset()
		if selectedLine < scrollOffset {
			d.scrollbar.SetScrollOffset(selectedLine)
		} else if selectedLine >= scrollOffset+visLines {
			d.scrollbar.SetScrollOffset(selectedLine - visLines + 1)
		}
		d.needsScrollToSel = false
	}

	// Slice visible lines based on scroll offset
	scrollOffset := d.scrollbar.GetScrollOffset()
	visibleEnd := min(scrollOffset+visLines, totalLines)
	var visibleCommandLines []string
	if totalLines > 0 {
		visibleCommandLines = allLines[scrollOffset:visibleEnd]
	}

	// Pad with empty lines if content is shorter than visible area
	for len(visibleCommandLines) < visLines {
		visibleCommandLines = append(visibleCommandLines, "")
	}

	// Handle empty state
	if len(d.filtered) == 0 {
		visibleCommandLines = []string{"", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No commands found")}
		for len(visibleCommandLines) < visLines {
			visibleCommandLines = append(visibleCommandLines, "")
		}
	}

	// Build command list with fixed width
	commandListStyle := lipgloss.NewStyle().Width(contentWidth)
	fixedWidthLines := make([]string, len(visibleCommandLines))
	for i, line := range visibleCommandLines {
		fixedWidthLines[i] = commandListStyle.Render(line)
	}
	commandListContent := strings.Join(fixedWidthLines, "\n")

	// Set scrollbar position for mouse hit testing
	dialogRow, dialogCol := d.Position()
	scrollbarX := dialogCol + dialogWidth - paletteScrollbarXInset - scrollbar.Width
	d.scrollbar.SetPosition(scrollbarX, dialogRow+paletteListStartY)

	// Combine content with scrollbar
	gap := strings.Repeat(" ", paletteScrollbarGap)
	scrollbarView := d.scrollbar.View()
	if scrollbarView == "" {
		scrollbarView = strings.Repeat(" ", scrollbar.Width)
	}
	scrollableContent := lipgloss.JoinHorizontal(lipgloss.Top, commandListContent, gap, scrollbarView)

	content := NewContent(contentWidth+paletteScrollbarGap+scrollbar.Width).
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
		// Calculate available width for description: contentWidth - label - " • " separator
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
