package todotool

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/tab"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/styles"
)

// SidebarComponent represents the todo display component for the sidebar
type SidebarComponent struct {
	todos []builtin.Todo
	width int
}

func NewSidebarComponent() *SidebarComponent {
	return &SidebarComponent{
		width: 20,
	}
}

func (c *SidebarComponent) SetSize(width int) {
	c.width = width
}

func (c *SidebarComponent) SetTodos(result *tools.ToolCallResult) error {
	if result == nil || result.Meta == nil {
		return nil
	}

	todos, ok := result.Meta.([]builtin.Todo)
	if !ok {
		return nil
	}

	c.todos = todos
	return nil
}

func (c *SidebarComponent) Render() string {
	if len(c.todos) == 0 {
		return ""
	}

	var lines []string
	for _, todo := range c.todos {
		lines = append(lines, c.renderTodoLine(todo))
	}

	return c.renderTab("TO-DO", strings.Join(lines, "\n"))
}

func (c *SidebarComponent) renderTodoLine(todo builtin.Todo) string {
	icon, style := renderTodoIcon(todo.Status)

	prefix := icon + " "
	prefixWidth := lipgloss.Width(prefix)
	maxDescWidth := max(1, c.width-prefixWidth)

	wrapped := toolcommon.WrapLinesWords(todo.Description, maxDescWidth)
	indent := strings.Repeat(" ", prefixWidth)

	var b strings.Builder
	for i, line := range wrapped {
		if i == 0 {
			b.WriteString(prefix + line)
		} else {
			b.WriteString("\n" + indent + line)
		}
	}

	return styles.TabPrimaryStyle.Render(style.Render(b.String()))
}

func (c *SidebarComponent) renderTab(title, content string) string {
	return tab.Render(title, content, c.width)
}
