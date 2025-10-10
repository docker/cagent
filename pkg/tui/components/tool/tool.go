package tool

import (
	"fmt"
	"log/slog"
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

	slog.Debug("Rendering tool message", "status", msg.ToolStatus, "content", msg.Content, "args", msg.ToolCall.Function.Arguments)
	slog.Debug("Tool definition", "name", msg.ToolDefinition.Name, "title", msg.ToolDefinition.Annotations.Title)
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolCall.Function.Arguments != "" {
		switch msg.ToolCall.Function.Name {
		case "search_files":
			content += " " + renderSearchFiles(msg.ToolCall)
		case "run_tools_with_javascript":
			content += " " + renderRunToolsWithJavascript(msg.ToolCall, mv.renderer)
		case "edit_file":
			diff, path := renderEditFile(msg.ToolCall, mv.width)
			if diff != "" {
				pathHeader := styles.HighlightStyle.Bold(true).Render(path)
				content += "\n" + pathHeader + "\n\n" + diff
			}
		default:
			lines := wrapLines(msg.ToolCall.Function.Arguments, min(120, mv.width-2))
			content += "\n" + strings.Join(lines, "\n")
		}
	}

	// Add spinner for pending and running tools
	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + mv.spinner.View()
	}

	// Add tool result content if available (for completed tools with content)
	var resultContent string
	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		style := styles.ToolCallResultStyle

		// Calculate available width for content (accounting for padding)
		padding := style.Padding().GetHorizontalPadding()
		availableWidth := max(mv.width-2-padding, 10) // Minimum readable width

		// Wrap long lines to fit the component width
		lines := wrapLines(msg.Content, availableWidth)

		// Take only first 10 lines after wrapping
		if len(lines) > 10 {
			lines = lines[:10]
			// Add indicator that content was truncated
			lines = append(lines, wrapLines("... (output truncated)", availableWidth)...)
		}

		// Join the lines back and apply muted style
		trimmedContent := strings.Join(lines, "\n")
		if trimmedContent != "" {
			resultContent = "\n" + style.Render(trimmedContent)
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
