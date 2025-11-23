package todotool

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	message *types.Message
	spinner spinner.Spinner
	width   int
	height  int
}

// New creates a new unified todo component.
// This component handles create, create_multiple, list, and update operations.
func New(
	msg *types.Message,
	_ *service.SessionState,
) layout.Model {
	return &Component{
		message: msg,
		spinner: spinner.New(spinner.ModeSpinnerOnly),
		width:   80,
		height:  1,
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
		panic("Unsupported todo tool: " + toolName)
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

	return styles.RenderComposite(styles.ToolMessageStyle.Width(c.width-1), content)
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

	return styles.RenderComposite(styles.ToolMessageStyle.Width(c.width-1), content)
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
				switch {
				case strings.Contains(line, "(Status: pending)"):
					icon, style := renderTodoIcon("pending")
					styledLines = append(styledLines, style.Render(icon)+" "+style.Render(strings.TrimSuffix(strings.TrimSpace(line[2:]), " (Status: pending)")))
				case strings.Contains(line, "(Status: in-progress)"):
					icon, style := renderTodoIcon("in-progress")
					styledLines = append(styledLines, style.Render(icon)+" "+style.Render(strings.TrimSuffix(strings.TrimSpace(line[2:]), " (Status: in-progress)")))
				case strings.Contains(line, "(Status: completed)"):
					icon, style := renderTodoIcon("completed")
					styledLines = append(styledLines, style.Render(icon)+" "+style.Render(strings.TrimSuffix(strings.TrimSpace(line[2:]), " (Status: completed)")))
				default:
					styledLines = append(styledLines, line)
				}
			} else {
				styledLines = append(styledLines, line)
			}
		}
		content += "\n" + strings.Join(styledLines, "\n")
	}

	return styles.RenderComposite(styles.ToolMessageStyle.Width(c.width-1), content)
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
		if err != nil {
			return ""
		}
		if updateParams, ok := params.(builtin.UpdateTodoArgs); ok {
			icon, style := renderTodoIcon(updateParams.Status)
			todoLine := fmt.Sprintf(" %s %s → %s",
				style.Render(icon),
				style.Render(updateParams.ID),
				style.Render(updateParams.Status))
			content += todoLine
		}
	}

	return styles.RenderComposite(styles.ToolMessageStyle.Width(c.width-1), content)
}
