package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/commands"
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
	width, height int
	textInput     textinput.Model
	categories    []commands.Category
	filtered      []commands.Item
	selected      int
	keyMap        commandPaletteKeyMap
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
	ti.Placeholder = "Type to search commands..."
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
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				selectedCmd := d.filtered[d.selected]
				cmds = append(cmds, core.CmdHandler(CloseDialogMsg{}))
				if selectedCmd.Execute != nil {
					cmds = append(cmds, selectedCmd.Execute())
				}
				return d, tea.Sequence(cmds...)
			}
			return d, nil

		case msg.String() == "ctrl+c":
			return d, tea.Quit

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
}

// View renders the command palette dialog
func (d *commandPaletteDialog) View() string {
	dialogWidth := max(min(d.width*80/100, 70), 80)

	maxHeight := min(d.height*70/100, 30)
	contentWidth := dialogWidth - 6

	title := styles.DialogTitleStyle.Width(contentWidth).Render("Commands")

	d.textInput.SetWidth(contentWidth)
	searchInput := d.textInput.View()

	separator := styles.DialogSeparatorStyle.
		Width(contentWidth).
		Render(strings.Repeat("─", contentWidth))

	var commandList []string
	maxItems := maxHeight - 8

	categoryMap := make(map[string][]commands.Item)
	categoryOrder := make([]string, 0)

	for _, cmd := range d.filtered {
		if _, exists := categoryMap[cmd.Category]; !exists {
			categoryOrder = append(categoryOrder, cmd.Category)
		}
		categoryMap[cmd.Category] = append(categoryMap[cmd.Category], cmd)
	}

	itemCount := 0
	currentIndex := 0

	for _, catName := range categoryOrder {
		if itemCount >= maxItems {
			break
		}

		commandList = append(commandList, styles.PaletteCategoryStyle.Render(catName))
		itemCount++

		for _, cmd := range categoryMap[catName] {
			if itemCount >= maxItems {
				break
			}

			isSelected := currentIndex == d.selected
			commandLine := d.renderCommand(cmd, isSelected)
			commandList = append(commandList, commandLine)
			itemCount++
			currentIndex++
		}
	}

	if len(d.filtered) == 0 {
		commandList = append(commandList, "", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No commands found"))
	}

	help := styles.DialogHelpStyle.
		Width(contentWidth).
		Render("↑/↓ navigate • enter execute • esc close")

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
func (d *commandPaletteDialog) renderCommand(cmd commands.Item, selected bool) string {
	text := "  " + cmd.Label
	if cmd.Description != "" {
		text += " - " + cmd.Description
	}

	if selected {
		return styles.PaletteSelectedStyle.Render(text)
	}

	return styles.PaletteUnselectedStyle.Render(text)
}

// Position calculates the position to center the dialog
func (d *commandPaletteDialog) Position() (row, col int) {
	dialogWidth := max(min(d.width*80/100, 70), 50)

	maxHeight := min(d.height*70/100, 30)

	// Center the dialog
	row = max(0, (d.height-maxHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}

// SetSize implements Dialog
func (d *commandPaletteDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}
