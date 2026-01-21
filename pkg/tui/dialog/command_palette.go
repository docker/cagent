package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/commands"
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
	textInput  textinput.Model
	categories []commands.Category
	filtered   []commands.Item
	selected   int
	offset     int // scroll offset for visible window
	keyMap     commandPaletteKeyMap
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
				d.ensureVisible()
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
				d.ensureVisible()
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				selectedCmd := d.filtered[d.selected]
				cmds = append(cmds, core.CmdHandler(CloseDialogMsg{}))
				if selectedCmd.Execute != nil {
					cmds = append(cmds, selectedCmd.Execute(""))
				}
				return d, tea.Sequence(cmds...)
			}
			return d, nil

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			cmds = append(cmds, cmd)
			d.filterCommands()
		}
	}

	return d, tea.Batch(cmds...)
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
		d.offset = 0
		return
	}

	d.filtered = make([]commands.Item, 0)
	for _, cat := range d.categories {
		for _, cmd := range cat.Commands {
			if strings.Contains(strings.ToLower(cmd.Label), query) ||
				strings.Contains(strings.ToLower(cmd.Description), query) ||
				strings.Contains(strings.ToLower(cmd.Category), query) {
				d.filtered = append(d.filtered, cmd)
			}
		}
	}

	if d.selected >= len(d.filtered) {
		d.selected = 0
	}
	d.offset = 0
}

// maxVisibleLines returns the maximum number of lines available for the command list
func (d *commandPaletteDialog) maxVisibleLines() int {
	maxHeight := min(d.Height()*70/100, 30)
	return max(1, maxHeight-8)
}

// ensureVisible adjusts the scroll offset so the selected item is visible
func (d *commandPaletteDialog) ensureVisible() {
	if d.selected < d.offset {
		d.offset = d.selected
		return
	}

	// Simulate rendering to check if selected item is visible
	maxLines := d.maxVisibleLines()
	for {
		lineCount := 0
		selectedVisible := false
		var lastCategory string

		for i := d.offset; i < len(d.filtered) && lineCount < maxLines; i++ {
			cmd := d.filtered[i]
			// Category header takes a line
			if cmd.Category != lastCategory {
				if lineCount >= maxLines {
					break
				}
				lineCount++
				lastCategory = cmd.Category
			}
			if lineCount >= maxLines {
				break
			}
			if i == d.selected {
				selectedVisible = true
			}
			lineCount++
		}

		if selectedVisible {
			break
		}
		// Selected item not visible, increase offset
		d.offset++
		if d.offset >= len(d.filtered) {
			d.offset = max(0, len(d.filtered)-1)
			break
		}
	}
}

// View renders the command palette dialog
func (d *commandPaletteDialog) View() string {
	dialogWidth := max(min(d.Width()*80/100, 70), 80)
	contentWidth := dialogWidth - 6

	title := RenderTitle("Commands", contentWidth, styles.DialogTitleStyle)

	d.textInput.SetWidth(contentWidth)
	searchInput := d.textInput.View()

	separator := RenderSeparator(contentWidth)

	var commandList []string
	maxLines := d.maxVisibleLines()

	// Render commands in the visible window, accounting for category headers
	lineCount := 0
	var lastCategory string
	for i := d.offset; i < len(d.filtered) && lineCount < maxLines; i++ {
		cmd := d.filtered[i]
		// Show category header when it changes
		if cmd.Category != lastCategory {
			if lineCount < maxLines {
				commandList = append(commandList, styles.PaletteCategoryStyle.Render(cmd.Category))
				lineCount++
				lastCategory = cmd.Category
			}
			if lineCount >= maxLines {
				break
			}
		}
		isSelected := i == d.selected
		commandLine := d.renderCommand(cmd, isSelected, contentWidth)
		commandList = append(commandList, commandLine)
		lineCount++
	}

	// Pad with empty lines to maintain consistent height
	for lineCount < maxLines {
		commandList = append(commandList, "")
		lineCount++
	}

	if len(d.filtered) == 0 {
		commandList = append(commandList, "", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No commands found"))
	}

	help := RenderHelpKeys(contentWidth, "↑/↓", "navigate", "enter", "execute", "esc", "close")

	parts := []string{
		title,
		"",
		searchInput,
		separator,
	}
	parts = append(parts, commandList...)
	parts = append(parts, "", help)

	return styles.DialogStyle.
		Width(dialogWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
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
	dialogWidth := max(min(d.Width()*80/100, 70), 50)
	maxHeight := min(d.Height()*70/100, 30)
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
