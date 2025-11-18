package todotool

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/docker/cagent/pkg/tui/types"
)

// TodoOutputParser provides robust parsing of todo tool outputs
type TodoOutputParser struct {
	todoLineRegex   *regexp.Regexp
	statusExtractor *regexp.Regexp
}

// ParsedTodo represents a parsed todo item from tool output
type ParsedTodo struct {
	ID          string
	Description string
	Status      string
	RawLine     string
}

// NewTodoOutputParser creates a new parser instance
func NewTodoOutputParser() *TodoOutputParser {
	return &TodoOutputParser{
		// Matches: "- [todo_1] Description (Status: pending)"
		todoLineRegex: regexp.MustCompile(`^-\s*\[([^\]]+)\]\s*(.+?)(?:\s*\(Status:\s*([^)]+)\))?\s*$`),
		// Extracts status from end of line
		statusExtractor: regexp.MustCompile(`\(Status:\s*([^)]+)\)\s*$`),
	}
}

// ParseTodoList parses the full output from list_todos tool
func (p *TodoOutputParser) ParseTodoList(output string) ([]ParsedTodo, error) {
	if output == "" {
		return []ParsedTodo{}, nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var todos []ParsedTodo

	for i, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "Current todos:") {
			continue
		}

		if todo, err := p.ParseTodoLine(line); err == nil {
			todos = append(todos, todo)
		} else {
			slog.Debug("Failed to parse todo line",
				"line_number", i+1,
				"line_content", line,
				"error", err)
		}
	}

	return todos, nil
}

// ParseTodoListWithFallback tries robust parsing first, then falls back to string-based parsing
func (p *TodoOutputParser) ParseTodoListWithFallback(output string) ([]ParsedTodo, error) {
	todos, err := p.ParseTodoList(output)
	if err == nil && len(todos) > 0 {
		return todos, nil
	}

	// this is the fallback if parsing fails or finds nothing
	slog.Debug("Robust parsing failed or found no todos, trying string-based parsing")
	return p.parseTodoListStringBased(output)
}

// parseTodoListStringBased implements string-based parsing as fallback
func (p *TodoOutputParser) parseTodoListStringBased(output string) ([]ParsedTodo, error) {
	if output == "" {
		return []ParsedTodo{}, nil
	}

	lines := strings.Split(output, "\n")
	var todos []ParsedTodo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [") {
			todo := ParsedTodo{
				RawLine: line,
				Status:  "pending", // default
			}

			// Extract todo content, removing the ID portion
			// Format: "- [todo_1] Description (Status: pending)"
			content := strings.TrimSpace(line[2:]) // Remove "- ["

			// Find the closing bracket to extract ID and description
			if closeIdx := strings.Index(content, "] "); closeIdx > 0 {
				todo.ID = content[:closeIdx]
				description := content[closeIdx+2:] // Everything after "] "

				switch {
				case strings.Contains(description, "(Status: pending)"):
					todo.Status = "pending"
					todo.Description = strings.TrimSuffix(description, " (Status: pending)")
				case strings.Contains(description, "(Status: in-progress)"):
					todo.Status = "in-progress"
					todo.Description = strings.TrimSuffix(description, " (Status: in-progress)")
				case strings.Contains(description, "(Status: completed)"):
					todo.Status = "completed"
					todo.Description = strings.TrimSuffix(description, " (Status: completed)")
				default:
					todo.Description = description
				}
			} else {
				// Fallback for unexpected format - treat whole content as description
				todo.Description = content
				todo.ID = ""
			}

			todo.Description = strings.TrimSpace(todo.Description)
			todos = append(todos, todo)
		}
	}

	return todos, nil
}

// ParseTodoLine parses a single todo line from the output
func (p *TodoOutputParser) ParseTodoLine(line string) (ParsedTodo, error) {
	line = strings.TrimSpace(line)

	todo := ParsedTodo{
		RawLine: line,
		Status:  "pending", // "pending" as default status
	}

	// format: "- [todo_1] Description (Status: pending)"
	if matches := p.todoLineRegex.FindStringSubmatch(line); len(matches) >= 3 {
		todo.ID = matches[1]
		todo.Description = strings.TrimSpace(matches[2])

		if len(matches) >= 4 && matches[3] != "" {
			todo.Status = strings.TrimSpace(matches[3])
		} else {
			// this extracts status from description
			if statusMatch := p.statusExtractor.FindStringSubmatch(todo.Description); len(statusMatch) >= 2 {
				todo.Status = strings.TrimSpace(statusMatch[1])
				todo.Description = p.statusExtractor.ReplaceAllString(todo.Description, "")
			}
		}

		todo.Description = strings.TrimSpace(todo.Description)
		return todo, nil
	}

	// Handle simple bullet format: "- Description"
	if strings.HasPrefix(line, "- ") {
		todo.Description = strings.TrimSpace(line[2:])
		todo.ID = ""
		return todo, nil
	}

	// Handle plain text format (only if non-empty)
	if line != "" {
		todo.Description = line
		todo.ID = ""
		return todo, nil
	}

	return todo, fmt.Errorf("unable to parse todo line: %q", line)
}

// RenderParsedTodo renders a ParsedTodo using the existing style system
func RenderParsedTodo(todo ParsedTodo) string {
	icon, style := renderTodoIcon(todo.Status)
	return style.Render(icon) + " " + style.Render(todo.Description)
}

// ConvertToTodoType converts ParsedTodo to the service Todo type
func (p ParsedTodo) ToTodoType() types.Todo {
	return types.Todo{
		ID:          p.ID,
		Description: p.Description,
		Status:      p.Status,
	}
}
