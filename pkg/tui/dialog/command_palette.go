package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/textinput"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core"
)

// CommandExecuteMsg is sent when a command is selected
type CommandExecuteMsg struct {
	Command Command
}

// Command represents a single command in the palette
type Command struct {
	ID          string
	Label       string
	Description string
	Category    string
	Execute     func() tea.Cmd
}

// CommandCategory represents a category of commands
type CommandCategory struct {
	Name     string
	Commands []Command
}

// commandPaletteDialog implements Dialog for the command palette
type commandPaletteDialog struct {
	width, height int
	textInput     textinput.Model
	categories    []CommandCategory
	filtered      []Command
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
func NewCommandPaletteDialog(categories []CommandCategory) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search commands..."
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)

	// Build initial filtered list (all commands)
	var allCommands []Command
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
func (d *commandPaletteDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				return d, tea.Batch(cmds...)
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
		d.filtered = make([]Command, 0)
		for _, cat := range d.categories {
			d.filtered = append(d.filtered, cat.Commands...)
		}
		d.selected = 0
		return
	}

	d.filtered = make([]Command, 0)
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
	dialogWidth := min(d.width*80/100, 70)
	if dialogWidth < 80 {
		dialogWidth = 80
	}

	maxHeight := min(d.height*70/100, 30)
	contentWidth := dialogWidth - 6

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Padding(1, 2).
		Width(dialogWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#9ca3af")).
		Width(contentWidth)
	title := titleStyle.Render("Commands")

	d.textInput.SetWidth(contentWidth)
	searchInput := d.textInput.View()

	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4b5563")).
		Width(contentWidth).
		Render(strings.Repeat("─", contentWidth))

	var commandList []string
	maxItems := (maxHeight - 8)

	categoryMap := make(map[string][]Command)
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

		commands := categoryMap[catName]

		categoryStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#6b7280")).
			MarginTop(1)
		commandList = append(commandList, categoryStyle.Render(catName))
		itemCount++

		for _, cmd := range commands {
			if itemCount >= maxItems {
				break
			}

			isSelected := currentIndex == d.selected
			commandLine := d.renderCommand(cmd, isSelected, contentWidth)
			commandList = append(commandList, commandLine)
			itemCount++
			currentIndex++
		}
	}

	if len(d.filtered) == 0 {
		noResultsStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280")).
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth)
		commandList = append(commandList, "", noResultsStyle.Render("No commands found"))
	}

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b7280")).
		Italic(true).
		MarginTop(1).
		Width(contentWidth)
	help := helpStyle.Render("↑/↓ navigate • enter execute • esc close")

	parts := []string{
		title,
		"",
		searchInput,
		separator,
	}
	parts = append(parts, commandList...)
	parts = append(parts, "", help)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return dialogStyle.Render(content)
}

// renderCommand renders a single command in the list
func (d *commandPaletteDialog) renderCommand(cmd Command, selected bool, width int) string {
	var style lipgloss.Style

	if selected {
		style = lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Foreground(lipgloss.Color("#f9fafb")).
			Bold(true).
			Width(width).
			Padding(0, 1)
	} else {
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d1d5db")).
			Width(width).
			Padding(0, 1)
	}

	labelStyle := lipgloss.NewStyle()
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))

	text := "  " + labelStyle.Render(cmd.Label)
	if cmd.Description != "" {
		text += " " + descStyle.Render("- "+cmd.Description)
	}

	return style.Render(text)
}

// Position calculates the position to center the dialog
func (d *commandPaletteDialog) Position() (row, col int) {
	dialogWidth := min(d.width*80/100, 70)
	if dialogWidth < 50 {
		dialogWidth = 50
	}

	maxHeight := min(d.height*70/100, 30)

	// Estimate dialog height
	dialogHeight := min(8+len(d.filtered), maxHeight)

	// Center the dialog
	row = max(0, (d.height-dialogHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}

// SetSize implements Dialog
func (d *commandPaletteDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}

// OpenCommandPalette returns a command to open the command palette
func OpenCommandPalette(categories []CommandCategory) tea.Cmd {
	return core.CmdHandler(OpenDialogMsg{
		Model: NewCommandPaletteDialog(categories),
	})
}
