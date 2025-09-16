package todo

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
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
		todos: make([]Todo, 0),
		width: 20,
	}
}

// SetSize sets the component width
func (c *Component) SetSize(width int) {
	c.width = width
}

// ParseTodoArguments extracts todos from tool call arguments
func (c *Component) ParseTodoArguments(toolName, arguments string) error {
	switch toolName {
	case "create_todo":
		var params struct {
			Description string `json:"description"`
		}
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
		var params struct {
			Todos []struct {
				Description string `json:"description"`
			} `json:"todos"`
		}
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return err
		}

		// Add all new todos
		for _, todoParam := range params.Todos {
			newTodo := Todo{
				ID:          fmt.Sprintf("todo_%d", len(c.todos)+1),
				Description: todoParam.Description,
				Status:      "pending",
			}
			c.todos = append(c.todos, newTodo)
		}

	case "update_todo":
		var params struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
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

// ParseTodoWriteArguments handles the todo_write tool arguments format
func (c *Component) ParseTodoWriteArguments(arguments string) error {
	var params struct {
		Merge bool `json:"merge"`
		Todos []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}

	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return err
	}

	if params.Merge {
		// Update existing todos
		for _, newTodo := range params.Todos {
			found := false
			for i, existingTodo := range c.todos {
				if existingTodo.ID == newTodo.ID {
					// Update existing todo
					if newTodo.Content != "" {
						c.todos[i].Description = newTodo.Content
					}
					if newTodo.Status != "" {
						c.todos[i].Status = newTodo.Status
					}
					found = true
					break
				}
			}
			if !found && newTodo.ID != "" {
				// Add new todo if not found
				c.todos = append(c.todos, Todo{
					ID:          newTodo.ID,
					Description: newTodo.Content,
					Status:      newTodo.Status,
				})
			}
		}
	} else {
		// Replace all todos
		c.todos = make([]Todo, 0, len(params.Todos))
		for _, newTodo := range params.Todos {
			c.todos = append(c.todos, Todo{
				ID:          newTodo.ID,
				Description: newTodo.Content,
				Status:      newTodo.Status,
			})
		}
	}

	return nil
}

// Render renders the todo component
func (c *Component) Render() string {
	if len(c.todos) == 0 {
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
