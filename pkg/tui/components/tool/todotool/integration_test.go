package todotool

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

// TestTodoComponentIntegration tests the full component lifecycle
// to ensure TUI doesn't break with various todo tool outputs
func TestTodoComponentIntegration(t *testing.T) {
	// Setup
	renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"))
	require.NoError(t, err)

	sessionState := &service.SessionState{
		TodoManager: service.NewTodoManager(),
	}

	tests := []struct {
		name           string
		toolName       string
		toolOutput     string
		toolStatus     types.ToolStatus
		expectedSubstr []string // Substrings that should appear in the rendered output
		shouldNotPanic bool
	}{
		{
			name:       "list todos with standard format",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: `Current todos:
- [todo_1] Fix authentication bug (Status: pending)
- [todo_2] Implement user dashboard (Status: in-progress)
- [todo_3] Write unit tests (Status: completed)`,
			expectedSubstr: []string{
				"List TODOs",
				"Fix authentication bug",
				"Implement user dashboard",
				"Write unit tests",
			},
			shouldNotPanic: true,
		},
		{
			name:       "list todos with mixed formats",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: `Current todos:
- [todo_1] Standard todo (Status: pending)
- Simple bullet todo
- Plain text todo
- [todo_2] Another standard todo (Status: completed)`,
			expectedSubstr: []string{
				"List TODOs",
				"Standard todo",
				"Simple bullet todo",
				"Plain text todo",
				"Another standard todo",
			},
			shouldNotPanic: true,
		},
		{
			name:       "list todos with malformed output",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: `Unexpected format
Some random text
- [broken format
- Valid todo`,
			expectedSubstr: []string{
				"List TODOs",
				"Valid todo",
			},
			shouldNotPanic: true,
		},
		{
			name:       "empty todo list",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: `Current todos:
`,
			expectedSubstr: []string{
				"List TODOs",
			},
			shouldNotPanic: true,
		},
		{
			name:       "create todo with arguments",
			toolName:   builtin.ToolNameCreateTodo,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: "Created todo [todo_1]: Fix authentication bug",
			expectedSubstr: []string{
				"Create TODO",
				"Fix authentication bug",
			},
			shouldNotPanic: true,
		},
		{
			name:       "create multiple todos",
			toolName:   builtin.ToolNameCreateTodos,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: "Created 3 todos: [todo_1], [todo_2], [todo_3]",
			expectedSubstr: []string{
				"Create TODOs",
			},
			shouldNotPanic: true,
		},
		{
			name:       "update todo",
			toolName:   builtin.ToolNameUpdateTodo,
			toolStatus: types.ToolStatusCompleted,
			toolOutput: `Updated todo "Fix authentication bug" to status: [completed]`,
			expectedSubstr: []string{
				"Update TODO",
			},
			shouldNotPanic: true,
		},
		{
			name:       "pending todo operation",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusPending,
			toolOutput: "",
			expectedSubstr: []string{
				"List TODOs",
			},
			shouldNotPanic: true,
		},
		{
			name:       "error in todo operation",
			toolName:   builtin.ToolNameListTodos,
			toolStatus: types.ToolStatusError,
			toolOutput: "Error: Could not list todos",
			expectedSubstr: []string{
				"List TODOs",
				"Error: Could not list todos",
			},
			shouldNotPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test message
			msg := &types.Message{
				Content:    tt.toolOutput,
				ToolStatus: tt.toolStatus,
				ToolCall: tools.ToolCall{
					Function: tools.FunctionCall{
						Name:      tt.toolName,
						Arguments: createTestArgs(tt.toolName),
					},
				},
				ToolDefinition: *createTestToolDefinition(tt.toolName),
			}

			// Create component
			component := New(msg, renderer, sessionState)

			// Test that component creation doesn't panic
			if tt.shouldNotPanic {
				assert.NotPanics(t, func() {
					component := component.(*Component)
					component.SetSize(80, 20)
				})
			}

			// Test that rendering doesn't panic
			var output string
			assert.NotPanics(t, func() {
				output = component.View()
			})

			// Verify expected substrings appear in output
			for _, substr := range tt.expectedSubstr {
				assert.Contains(t, output, substr,
					"Expected substring %q not found in output: %s", substr, output)
			}

			// Verify output is not empty for completed operations
			if tt.toolStatus == types.ToolStatusCompleted {
				assert.NotEmpty(t, output, "Output should not be empty for completed operations")
			}

			// Verify output contains some basic formatting
			assert.NotEmpty(t, output, "Output should contain some content")
		})
	}
}

// TestTodoComponentStressTest tests component with extreme inputs
func TestTodoComponentStressTest(t *testing.T) {
	renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"))
	require.NoError(t, err)

	sessionState := &service.SessionState{
		TodoManager: service.NewTodoManager(),
	}

	stressTests := []struct {
		name       string
		toolOutput string
	}{
		{
			name:       "very long todo output",
			toolOutput: generateLongTodoOutput(1000),
		},
		{
			name: "output with special characters",
			toolOutput: `Current todos:
- [todo_1] Fix authentication "bug" & handle <script>alert('xss')</script> (Status: pending)
- [todo_2] Implement ñew feature with éspecial chars (Status: in-progress)`,
		},
		{
			name:       "extremely malformed output",
			toolOutput: `][{|}@#$%^&*()_+=-0987654321`,
		},
		{
			name:       "empty output",
			toolOutput: "",
		},
		{
			name:       "output with null bytes",
			toolOutput: "Current todos:\x00\n- [todo_1] Test\x00todo (Status: pending)",
		},
	}

	for _, tt := range stressTests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &types.Message{
				Content:    tt.toolOutput,
				ToolStatus: types.ToolStatusCompleted,
				ToolCall: tools.ToolCall{
					Function: tools.FunctionCall{
						Name:      builtin.ToolNameListTodos,
						Arguments: "{}",
					},
				},
				ToolDefinition: *createTestToolDefinition(builtin.ToolNameListTodos),
			}

			component := New(msg, renderer, sessionState)

			// Should not panic with any input
			assert.NotPanics(t, func() {
				component.SetSize(80, 20)
				output := component.View()
				// Should always produce some output
				assert.NotEmpty(t, output)
			})
		})
	}
}

// Helper functions for tests

func createTestArgs(toolName string) string {
	switch toolName {
	case builtin.ToolNameCreateTodo:
		return `{"description": "Fix authentication bug"}`
	case builtin.ToolNameCreateTodos:
		return `{"descriptions": ["Task 1", "Task 2", "Task 3"]}`
	case builtin.ToolNameUpdateTodo:
		return `{"id": "todo_1", "status": "completed"}`
	case builtin.ToolNameListTodos:
		return "{}"
	default:
		return "{}"
	}
}

func createTestToolDefinition(toolName string) *tools.Tool {
	var title string
	switch toolName {
	case builtin.ToolNameCreateTodo:
		title = "Create TODO"
	case builtin.ToolNameCreateTodos:
		title = "Create TODOs"
	case builtin.ToolNameUpdateTodo:
		title = "Update TODO"
	case builtin.ToolNameListTodos:
		title = "List TODOs"
	default:
		title = "Unknown Tool"
	}

	return &tools.Tool{
		Name: toolName,
		Annotations: tools.ToolAnnotations{
			Title: title,
		},
	}
}

func generateLongTodoOutput(numTodos int) string {
	var output strings.Builder
	output.WriteString("Current todos:\n")

	for i := 1; i <= numTodos; i++ {
		status := "pending"
		if i%3 == 0 {
			status = "completed"
		} else if i%2 == 0 {
			status = "in-progress"
		}

		output.WriteString("- [todo_")
		output.WriteString(string(rune('0' + (i % 10))))
		output.WriteString("] Very long todo description that might cause issues with rendering number ")
		output.WriteString(string(rune('0' + (i % 10))))
		output.WriteString(" and contains lots of text to stress test the component (Status: ")
		output.WriteString(status)
		output.WriteString(")\n")
	}

	return output.String()
}
