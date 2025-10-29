package dialog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/todo"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/styles"
)

// ToolConfirmationResponse represents the user's response to tool confirmation
type ToolConfirmationResponse struct {
	Response string // "approve", "reject", or "approve-session"
}

// toolConfirmationDialog implements DialogModel for tool confirmation
type toolConfirmationDialog struct {
	width, height int
	toolCall      tools.ToolCall
	app           *app.App
	keyMap        toolConfirmationKeyMap
}

// SetSize implements Dialog.
func (d *toolConfirmationDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}

// toolConfirmationKeyMap defines key bindings for tool confirmation dialog
type toolConfirmationKeyMap struct {
	Yes key.Binding
	No  key.Binding
	All key.Binding
}

// defaultToolConfirmationKeyMap returns default key bindings
func defaultToolConfirmationKeyMap() toolConfirmationKeyMap {
	return toolConfirmationKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "approve"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("N", "reject"),
		),
		All: key.NewBinding(
			key.WithKeys("a", "A"),
			key.WithHelp("A", "approve all"),
		),
	}
}

// NewToolConfirmationDialog creates a new tool confirmation dialog
func NewToolConfirmationDialog(toolCall tools.ToolCall, appInstance *app.App) Dialog {
	return &toolConfirmationDialog{
		toolCall: toolCall,
		app:      appInstance,
		keyMap:   defaultToolConfirmationKeyMap(),
	}
}

// Init initializes the tool confirmation dialog
func (d *toolConfirmationDialog) Init() tea.Cmd {
	return nil
}

// Update handles messages for the tool confirmation dialog
func (d *toolConfirmationDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case key.Matches(msg, d.keyMap.All):
			if d.app != nil {
				d.app.Resume("approve-session")
			}
			return d, core.CmdHandler(CloseDialogMsg{})
		}

		if msg.String() == "ctrl+c" {
			return d, tea.Quit
		}
	}

	return d, nil
}

// wrapText wraps text to fit within the specified width
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var result strings.Builder
	var currentLine strings.Builder

	for _, word := range words {
		switch {
		case currentLine.Len() == 0:
			currentLine.WriteString(word)
		case currentLine.Len()+1+len(word) <= width:
			currentLine.WriteString(" " + word)
		default:
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(currentLine.String())
			currentLine.Reset()
			currentLine.WriteString(word)
		}
	}

	if currentLine.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString(currentLine.String())
	}

	return result.String()
}

// View renders the tool confirmation dialog
func (d *toolConfirmationDialog) View() string {
	// Calculate dialog width based on screen size but with min/max constraints
	dialogWidth := d.width * 60 / 100 // 60% of screen width
	if dialogWidth < 50 {
		dialogWidth = 50
	}
	if dialogWidth > 80 {
		dialogWidth = 80
	}

	// Content width (accounting for padding and borders)
	contentWidth := dialogWidth - 6

	dialogStyle := styles.DialogStyle.Width(dialogWidth)

	// Title
	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")

	// Separator
	separatorWidth := max(contentWidth-10, 20)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))

	// Tool name
	toolLabel := styles.DialogLabelStyle.Render("Tool: ")
	toolName := styles.DialogValueStyle.Render(d.toolCall.Function.Name)
	toolInfo := lipgloss.JoinHorizontal(lipgloss.Left, toolLabel, toolName)

	// Arguments section
	var argumentsSection string
	if d.toolCall.Function.Name == "create_todos" || d.toolCall.Function.Name == "create_todo" {
		argumentsSection = d.renderTodo(contentWidth)
	} else {
		argumentsSection = d.renderArguments(contentWidth)
	}

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	options := styles.DialogOptionsStyle.Width(contentWidth).Render("[Y]es    [N]o    [A]ll (approve all tools this session)")

	// Combine all parts with proper spacing
	parts := []string{title, separator, toolInfo}

	if argumentsSection != "" {
		parts = append(parts, "", argumentsSection)
	}

	parts = append(parts, "", question, "", options)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return dialogStyle.Render(content)
}

func (d *toolConfirmationDialog) renderTodo(contentWidth int) string {
	todoComponent := todo.NewComponent()
	todoComponent.SetSize(contentWidth)

	err := todoComponent.SetTodos(d.toolCall)
	if err != nil {
		return ""
	}
	return todoComponent.Render()
}

func (d *toolConfirmationDialog) renderArguments(contentWidth int) string {
	if d.toolCall.Function.Arguments == "" {
		return ""
	}

	argumentsHeader := styles.DialogLabelStyle.Render("Arguments:")

	var arguments map[string]any
	if err := json.Unmarshal([]byte(d.toolCall.Function.Arguments), &arguments); err != nil {
		// If JSON unmarshaling fails, truncate and wrap the raw arguments
		rawArgs := d.toolCall.Function.Arguments
		if len(rawArgs) > 150 {
			rawArgs = rawArgs[:150] + "..."
		}

		formattedArgs := styles.DialogContentStyle.Render("  " + wrapText(rawArgs, contentWidth-2))
		return lipgloss.JoinVertical(lipgloss.Left, argumentsHeader, "", formattedArgs)
	}

	// Sort arguments by key to ensure consistent order
	keys := make([]string, 0, len(arguments))
	for k := range arguments {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var argLines []string
	for _, k := range keys {
		v := arguments[k]

		// Format key
		formattedKey := styles.DialogValueStyle.Render(k + ":")

		// Format value
		var valueStr string
		if vStr, ok := v.(string); ok {
			valueStr = vStr
		} else {
			valueBytes, _ := json.MarshalIndent(v, "", "  ")
			valueStr = string(valueBytes)
		}

		// Truncate very long values before wrapping
		maxValueLength := 200
		if len(valueStr) > maxValueLength {
			valueStr = valueStr[:maxValueLength] + "..."
		}

		// Wrap long values
		// Account for key, colon, spaces, and indent
		availableWidth := max(contentWidth-len(k)-6, 20)
		wrappedValue := wrapText(valueStr, availableWidth)

		// Limit to maximum 3 lines for readability
		valueLines := strings.Split(wrappedValue, "\n")
		if len(valueLines) > 3 {
			valueLines = valueLines[:3]
			if len(valueLines[2]) > availableWidth-3 {
				valueLines[2] = valueLines[2][:availableWidth-3] + "..."
			} else {
				valueLines[2] += "..."
			}
			wrappedValue = strings.Join(valueLines, "\n")
		}

		// Indent wrapped lines
		valueLines = strings.Split(wrappedValue, "\n")
		if len(valueLines) > 1 {
			for i := 1; i < len(valueLines); i++ {
				valueLines[i] = "    " + valueLines[i]
			}
			wrappedValue = strings.Join(valueLines, "\n")
		}

		valueStyled := styles.DialogContentStyle.Render(wrappedValue)

		argLines = append(argLines, fmt.Sprintf("  %s %s", formattedKey, valueStyled))
	}

	formattedArgs := strings.Join(argLines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, argumentsHeader, "", formattedArgs)
}

// Position calculates the position to center the dialog
func (d *toolConfirmationDialog) Position() (row, col int) {
	// Calculate dialog width (same logic as in View())
	dialogWidth := min(max(d.width*60/100, 50), 80)

	// Estimate dialog height based on content
	dialogHeight := 12 // Base height for title, tool name, question, and options
	if d.toolCall.Function.Arguments != "" {
		// Add height for arguments section
		// Rough estimation: 3 lines for header + arguments
		dialogHeight += 5
	}

	// Add height for todo preview section if todo-related tools
	if d.toolCall.Function.Name == "create_todos" || d.toolCall.Function.Name == "create_todo" && d.toolCall.Function.Arguments != "" {
		// Add height for preview section header and content
		// Rough estimation: 2 lines for header + variable lines for todos
		dialogHeight += 6
	}

	// Ensure dialog stays on screen
	row = max(0, (d.height-dialogHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}
