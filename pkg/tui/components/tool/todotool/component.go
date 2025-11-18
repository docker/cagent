package todotool

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour/v2"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Component represents a unified todo tool component that handles all todo operations.
// It determines which operation to display based on the tool call name.
type Component struct {
	message      *types.Message
	renderer     *glamour.TermRenderer
	sessionState *service.SessionState
	spinner      spinner.Spinner
	width        int
	height       int
}

// New creates a new unified todo component.
// This component handles create, create_multiple, list, and update operations.
func New(
	msg *types.Message,
	renderer *glamour.TermRenderer,
	sessionState *service.SessionState,
) layout.Model {
	return &Component{
		message:      msg,
		renderer:     renderer,
		sessionState: sessionState,
		spinner:      spinner.New(spinner.ModeSpinnerOnly),
		width:        80,
		height:       1,
	}
}

func (c *Component) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	return nil
}

func (c *Component) Init() tea.Cmd {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		return c.spinner.Init()
	}
	return nil
}

func (c *Component) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		var cmd tea.Cmd
		var model layout.Model
		model, cmd = c.spinner.Update(msg)
		c.spinner = model.(spinner.Spinner)
		return c, cmd
	}

	return c, nil
}

func (c *Component) View() string {
	msg := c.message
	toolName := msg.ToolCall.Function.Name

	// Render based on tool type
	switch toolName {
	case builtin.ToolNameCreateTodo:
		return c.renderCreate()
	case builtin.ToolNameCreateTodos:
		return c.renderCreateMultiple()
	case builtin.ToolNameListTodos:
		return c.renderList()
	case builtin.ToolNameUpdateTodo:
		return c.renderUpdate()
	default:
		return c.renderDefault()
	}
}

func (c *Component) renderCreate() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + c.spinner.View()
	}

	if msg.ToolCall.Function.Arguments != "" {
		params, err := parseTodoArgs(msg.ToolCall)
		if err == nil {
			if createParams, ok := params.(builtin.CreateTodoArgs); ok {
				icon, style := renderTodoIcon("pending")
				todoLine := fmt.Sprintf("\n%s %s", style.Render(icon), style.Render(createParams.Description))
				content += todoLine
			}
		}
	}

	var resultContent string
	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		resultContent = "\n" + styles.MutedStyle.Render(msg.Content)
	}

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content + resultContent)
}

func (c *Component) renderCreateMultiple() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + c.spinner.View()
	}

	if msg.ToolCall.Function.Arguments != "" {
		params, err := parseTodoArgs(msg.ToolCall)
		if err == nil {
			if createParams, ok := params.(builtin.CreateTodosArgs); ok {
				icon, style := renderTodoIcon("pending")
				for _, desc := range createParams.Descriptions {
					todoLine := fmt.Sprintf("\n%s %s", style.Render(icon), style.Render(desc))
					content += todoLine
				}
			}
		}
	}

	// For create operations, don't show redundant tool output since UI already lists todos
	var resultContent string
	// Skip showing tool output for createTodos since UI already displays individual todos

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content + resultContent)
}

func (c *Component) renderList() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + c.spinner.View()
	}

	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		lines := strings.Split(msg.Content, "\n")
		var styledLines []string
		for _, line := range lines {
			if strings.HasPrefix(line, "- [") {
				// Extract todo content, removing the ID portion
				// Format: "- [todo_1] Description (Status: pending)"
				content := strings.TrimSpace(line[2:]) // Remove "- ["

				// Find the closing bracket to extract ID and description
				if closeIdx := strings.Index(content, "] "); closeIdx > 0 {
					description := content[closeIdx+2:] // Everything after "] "

					switch {
					case strings.Contains(description, "(Status: pending)"):
						icon, style := renderTodoIcon("pending")
						desc := strings.TrimSuffix(description, " (Status: pending)")
						styledLines = append(styledLines, style.Render(icon)+" "+style.Render(desc))
					case strings.Contains(description, "(Status: in-progress)"):
						icon, style := renderTodoIcon("in-progress")
						desc := strings.TrimSuffix(description, " (Status: in-progress)")
						styledLines = append(styledLines, style.Render(icon)+" "+style.Render(desc))
					case strings.Contains(description, "(Status: completed)"):
						icon, style := renderTodoIcon("completed")
						desc := strings.TrimSuffix(description, " (Status: completed)")
						styledLines = append(styledLines, style.Render(icon)+" "+style.Render(desc))
					default:
						styledLines = append(styledLines, description)
					}
				} else {
					// Fallback for unexpected format
					styledLines = append(styledLines, line)
				}
			} else {
				styledLines = append(styledLines, line)
			}
		}
		content += "\n" + strings.Join(styledLines, "\n")
	}

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content)
}

func (c *Component) renderUpdate() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + c.spinner.View()
	}

	if msg.ToolCall.Function.Arguments != "" {
		params, err := parseTodoArgs(msg.ToolCall)
		if err == nil {
			if updateParams, ok := params.(builtin.UpdateTodoArgs); ok {
				icon, style := renderTodoIcon(updateParams.Status)

				// Try to get the todo description from the TodoManager
				var displayText string
				if c.sessionState != nil {
					if todo := c.sessionState.TodoManager.GetTodoByID(updateParams.ID); todo != nil {
						displayText = todo.Description
					} else {
						// Try to extract description from tool result content as fallback
						if msg.Content != "" && strings.Contains(msg.Content, "Updated todo ") {
							// Extract description from "Updated todo \"description\" to status: [status]"
							start := strings.Index(msg.Content, "Updated todo \"")
							if start >= 0 {
								start += 14 // length of "Updated todo \""
								end := strings.Index(msg.Content[start:], "\" to status:")
								if end > 0 {
									displayText = msg.Content[start : start+end]
								} else {
									displayText = "Task"
								}
							} else {
								displayText = "Task"
							}
						} else {
							displayText = "Task"
						}
					}
				} else {
					// Try to extract from tool result content without session state
					if msg.Content != "" && strings.Contains(msg.Content, "Updated todo ") {
						start := strings.Index(msg.Content, "Updated todo \"")
						if start >= 0 {
							start += 14 // length of "Updated todo \""
							end := strings.Index(msg.Content[start:], "\" to status:")
							if end > 0 {
								displayText = msg.Content[start : start+end]
							} else {
								displayText = "Task"
							}
						} else {
							displayText = "Task"
						}
					} else {
						displayText = "Task"
					}
				}

				todoLine := fmt.Sprintf("\n%s %s → %s",
					style.Render(icon),
					style.Render(displayText),
					style.Render(updateParams.Status))
				content += todoLine
			}
		}
	}

	// For update operations, don't show redundant tool output since UI already shows the result
	var resultContent string
	// Skip showing tool output for update operations as it's redundant
	// The UI already shows "description → status" which is more informative

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content + resultContent)
}

func (c *Component) renderDefault() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()
	content := fmt.Sprintf("%s %s", toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName))

	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		content += " " + c.spinner.View()
	}

	var resultContent string
	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		resultContent = "\n" + styles.MutedStyle.Render(msg.Content)
	}

	return styles.BaseStyle.PaddingLeft(2).PaddingTop(1).Render(content + resultContent)
}
