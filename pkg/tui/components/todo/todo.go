package todo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/styles"
)

// Todo represents a single todo item
type Todo struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// Component represents the todo display component
type Component struct {
	todos []Todo
	width int
}

// NewComponent creates a new todo component
func NewComponent() *Component {
	return &Component{
		width: 20,
	}
}

// SetSize sets the component width
func (c *Component) SetSize(width int) {
	c.width = width
}

// SetTodos sets the todo builtin call, handles create_todo, create_todos, update_todo
func (c *Component) SetTodos(toolCall tools.ToolCall) error {
	toolName := toolCall.Function.Name
	arguments := toolCall.Function.Arguments
	switch toolName {
	case "create_todo":
		var params builtin.CreateTodoArgs
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return err
		}

		// Add the new todo
		newTodo := Todo{
			ID:          fmt.Sprintf("todo_%d", len(c.todos)+1),
			Description: params.Description,
			Status:      "pending",
		}
		c.todos = append(c.todos, newTodo)

	case "create_todos":
		var params builtin.CreateTodosArgs
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return err
		}

		// Add all new todos
		for _, todoParam := range params.Descriptions {
			newTodo := Todo{
				ID:          fmt.Sprintf("todo_%d", len(c.todos)+1),
				Description: todoParam,
				Status:      "pending",
			}
			c.todos = append(c.todos, newTodo)
		}

	case "update_todo":
		var params builtin.UpdateTodoArgs
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return err
		}

		// Update existing todo
		for i, todo := range c.todos {
			if todo.ID == params.ID {
				c.todos[i].Status = params.Status
				break
			}
		}
	}

	return nil
}

// Render renders the todo component
func (c *Component) Render() string {
	if len(c.todos) == 0 {
		slog.Debug("No todos to render")
		return ""
	}

	var content strings.Builder
	content.WriteString(styles.HighlightStyle.Render("TODOs"))
	content.WriteString("\n")

	for _, todo := range c.todos {
		var icon string
		var style lipgloss.Style

		switch todo.Status {
		case "pending":
			icon = "◯"
			style = styles.PendingStyle
		case "in-progress":
			icon = "◕"
			style = styles.InProgressStyle
		case "completed":
			icon = "✓"
			style = styles.MutedStyle
		default:
			icon = "?"
			style = styles.BaseStyle
		}

		// Truncate description to fit width
		description := todo.Description
		maxDescWidth := max(c.width-2, 3)
		if len(description) > maxDescWidth {
			description = description[:maxDescWidth-3] + "..."
		}

		// Render icon and description separately for better control
		styledIcon := style.Render(icon)
		styledDescription := style.Render(description)
		content.WriteString(fmt.Sprintf("%s %s\n", styledIcon, styledDescription))
	}

	return content.String()
}
