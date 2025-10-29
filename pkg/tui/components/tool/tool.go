package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/glamour/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// toolModel implements Model
type toolModel struct {
	message *types.Message

	spinner  spinner.Model
	renderer *glamour.TermRenderer

	width  int
	height int

	app *app.App
}

// SetSize implements Model.
func (mv *toolModel) SetSize(width, height int) tea.Cmd {
	mv.width = width
	mv.height = height
	return nil
}

// New creates a new tool view
func New(msg *types.Message, a *app.App, renderer *glamour.TermRenderer) layout.Model {
	if msg.ToolCall.Function.Name == "transfer_task" {
		return &transferTaskModel{
			msg: msg,
		}
	}

	return &toolModel{
		message:  msg,
		width:    80,
		height:   1,
		spinner:  spinner.New(spinner.WithSpinner(spinner.Points)),
		renderer: renderer,
		app:      a,
	}
}

// Bubble Tea Model methods

// Init initializes the message view
func (mv *toolModel) Init() tea.Cmd {
	// Start spinner for empty assistant messages or pending/running tools
	switch mv.message.Type {
	case types.MessageTypeAssistant:
		if mv.message.Content == "" {
			return mv.spinner.Tick
		}
	case types.MessageTypeToolCall:
		if mv.message.ToolStatus == types.ToolStatusPending || mv.message.ToolStatus == types.ToolStatusRunning {
			return mv.spinner.Tick
		}
	}
	return nil
}

// Update handles messages and updates the message view state
func (mv *toolModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle spinner updates for empty assistant messages or pending/running tools
	switch mv.message.Type {
	case types.MessageTypeAssistant:
		if mv.message.Content == "" {
			var cmd tea.Cmd
			mv.spinner, cmd = mv.spinner.Update(msg)
			return mv, cmd
		}
	case types.MessageTypeToolCall:
		if mv.message.ToolStatus == types.ToolStatusPending || mv.message.ToolStatus == types.ToolStatusRunning {
			var cmd tea.Cmd
			mv.spinner, cmd = mv.spinner.Update(msg)
			return mv, cmd
		}
	}

	return mv, nil
}

func (mv *toolModel) View() string {
	msg := mv.message

	displayName := msg.ToolDefinition.DisplayName()

	content := fmt.Sprintf("%s %s", icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	// Add spinner for pending and running tools
	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + mv.spinner.View()
	}

	if msg.ToolCall.Function.Arguments != "" {
		switch msg.ToolCall.Function.Name {
		case "edit_file":
			diff, path := renderEditFile(msg.ToolCall, mv.width-4)
			if diff != "" {
				var editFile string
				editFile += styles.ToolCallArgKey.Render("path:")
				editFile += "\n" + path
				editFile += "\n\n" + diff
				content += "\n\n" + styles.ToolCallResult.Render(editFile)
			}
		default:
			content += "\n" + renderToolArgs(msg.ToolCall, mv.width-3)
		}
	}

	// Add tool result content if available (for completed tools with content)
	var resultContent string
	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		var content string
		var m map[string]any
		if err := json.Unmarshal([]byte(msg.Content), &m); err != nil {
			content = msg.Content
		} else if buf, err := json.MarshalIndent(m, "", "  "); err != nil {
			content = msg.Content
		} else {
			content = string(buf)
		}

		// Calculate available width for content (accounting for padding)
		padding := styles.ToolCallResult.Padding().GetHorizontalPadding()
		availableWidth := max(mv.width-2-padding, 10) // Minimum readable width

		// Wrap long lines to fit the component width
		lines := wrapLines(content, availableWidth)

		// Take only first 10 lines after wrapping
		header := "output"
		if len(lines) > 10 {
			lines = lines[:10]
			header = "output (truncated)"
			lines = append(lines, wrapLines("...", availableWidth)...)
		}

		// Join the lines back
		trimmedContent := strings.Join(lines, "\n")
		if trimmedContent != "" {
			resultContent = "\n" + styles.ToolCallResult.Render(styles.ToolCallResultKey.Render("\n-> "+header+":")+"\n"+trimmedContent)
		}
	}

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content + resultContent)
}

func icon(status types.ToolStatus) string {
	switch status {
	case types.ToolStatusPending:
		return "⊙"
	case types.ToolStatusRunning:
		return "⚙"
	case types.ToolStatusCompleted:
		return styles.SuccessStyle.Render("✓")
	case types.ToolStatusError:
		return styles.ErrorStyle.Render("✗")
	case types.ToolStatusConfirmation:
		return styles.WarningStyle.Render("?")
	default:
		return styles.WarningStyle.Render("?")
	}
}
