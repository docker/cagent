package todotool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoOutputParser_ParseTodoLine(t *testing.T) {
	parser := NewTodoOutputParser()

	tests := []struct {
		name        string
		input       string
		expected    ParsedTodo
		shouldError bool
	}{
		{
			name:  "standard format with pending status",
			input: "- [todo_1] Fix authentication bug (Status: pending)",
			expected: ParsedTodo{
				ID:          "todo_1",
				Description: "Fix authentication bug",
				Status:      "pending",
				RawLine:     "- [todo_1] Fix authentication bug (Status: pending)",
			},
		},
		{
			name:  "standard format with in-progress status",
			input: "- [todo_2] Implement user dashboard (Status: in-progress)",
			expected: ParsedTodo{
				ID:          "todo_2",
				Description: "Implement user dashboard",
				Status:      "in-progress",
				RawLine:     "- [todo_2] Implement user dashboard (Status: in-progress)",
			},
		},
		{
			name:  "standard format with completed status",
			input: "- [todo_3] Write unit tests (Status: completed)",
			expected: ParsedTodo{
				ID:          "todo_3",
				Description: "Write unit tests",
				Status:      "completed",
				RawLine:     "- [todo_3] Write unit tests (Status: completed)",
			},
		},
		{
			name:  "format without status defaults to pending",
			input: "- [todo_4] Review code changes",
			expected: ParsedTodo{
				ID:          "todo_4",
				Description: "Review code changes",
				Status:      "pending",
				RawLine:     "- [todo_4] Review code changes",
			},
		},
		{
			name:  "simple bullet format",
			input: "- Fix the login form validation",
			expected: ParsedTodo{
				ID:          "",
				Description: "Fix the login form validation",
				Status:      "pending",
				RawLine:     "- Fix the login form validation",
			},
		},
		{
			name:  "extra whitespace is trimmed",
			input: "   - [todo_5]    Refactor API endpoints   (Status: in-progress)   ",
			expected: ParsedTodo{
				ID:          "todo_5",
				Description: "Refactor API endpoints",
				Status:      "in-progress",
				RawLine:     "- [todo_5]    Refactor API endpoints   (Status: in-progress)",
			},
		},
		{
			name:  "status passed through as-is",
			input: "- [todo_6] Update documentation (Status: Done)",
			expected: ParsedTodo{
				ID:          "todo_6",
				Description: "Update documentation",
				Status:      "Done",
				RawLine:     "- [todo_6] Update documentation (Status: Done)",
			},
		},
		{
			name:  "alternate status format passed through",
			input: "- [todo_7] Deploy to production (Status: In Progress)",
			expected: ParsedTodo{
				ID:          "todo_7",
				Description: "Deploy to production",
				Status:      "In Progress",
				RawLine:     "- [todo_7] Deploy to production (Status: In Progress)",
			},
		},
		{
			name:        "empty line should error",
			input:       "",
			shouldError: true,
		},
		{
			name:  "header line treated as plain text",
			input: "Current todos:",
			expected: ParsedTodo{
				ID:          "",
				Description: "Current todos:",
				Status:      "pending",
				RawLine:     "Current todos:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseTodoLine(tt.input)

			if tt.shouldError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			assert.Equal(t, tt.expected.ID, result.ID)

			assert.Equal(t, tt.expected.Description, result.Description)
			assert.Equal(t, tt.expected.Status, result.Status)
			assert.Equal(t, strings.TrimSpace(tt.input), result.RawLine)
		})
	}
}

func TestTodoOutputParser_ParseTodoList(t *testing.T) {
	parser := NewTodoOutputParser()

	tests := []struct {
		name     string
		input    string
		expected []ParsedTodo
	}{
		{
			name: "full todo list output",
			input: `Current todos:
- [todo_1] Fix authentication bug (Status: pending)
- [todo_2] Implement user dashboard (Status: in-progress)
- [todo_3] Write unit tests (Status: completed)`,
			expected: []ParsedTodo{
				{
					ID:          "todo_1",
					Description: "Fix authentication bug",
					Status:      "pending",
					RawLine:     "- [todo_1] Fix authentication bug (Status: pending)",
				},
				{
					ID:          "todo_2",
					Description: "Implement user dashboard",
					Status:      "in-progress",
					RawLine:     "- [todo_2] Implement user dashboard (Status: in-progress)",
				},
				{
					ID:          "todo_3",
					Description: "Write unit tests",
					Status:      "completed",
					RawLine:     "- [todo_3] Write unit tests (Status: completed)",
				},
			},
		},
		{
			name: "mixed format todos",
			input: `Current todos:
- [todo_1] Fix authentication bug (Status: pending)
- Simple todo without ID
- [todo_2] Another structured todo`,
			expected: []ParsedTodo{
				{
					ID:          "todo_1",
					Description: "Fix authentication bug",
					Status:      "pending",
					RawLine:     "- [todo_1] Fix authentication bug (Status: pending)",
				},
				{
					ID:          "",
					Description: "Simple todo without ID",
					Status:      "pending",
					RawLine:     "- Simple todo without ID",
				},
				{
					ID:          "todo_2",
					Description: "Another structured todo",
					Status:      "pending",
					RawLine:     "- [todo_2] Another structured todo",
				},
			},
		},
		{
			name:     "empty output",
			input:    "",
			expected: []ParsedTodo{},
		},
		{
			name: "only headers",
			input: `Current todos:

			`,
			expected: []ParsedTodo{},
		},
		{
			name: "malformed lines are processed as plain text",
			input: `Current todos:
- [todo_1] Valid todo (Status: pending)
This is not a todo line
- [todo_2] Another valid todo`,
			expected: []ParsedTodo{
				{
					ID:          "todo_1",
					Description: "Valid todo",
					Status:      "pending",
					RawLine:     "- [todo_1] Valid todo (Status: pending)",
				},
				{
					ID:          "",
					Description: "This is not a todo line",
					Status:      "pending",
					RawLine:     "This is not a todo line",
				},
				{
					ID:          "todo_2",
					Description: "Another valid todo",
					Status:      "pending",
					RawLine:     "- [todo_2] Another valid todo",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseTodoList(tt.input)
			require.NoError(t, err)
			assert.Len(t, result, len(tt.expected))

			for i, expected := range tt.expected {
				if i < len(result) {
					assert.Equal(t, expected.ID, result[i].ID)
					assert.Equal(t, expected.Description, result[i].Description)
					assert.Equal(t, expected.Status, result[i].Status)
					assert.Equal(t, expected.RawLine, result[i].RawLine)
				}
			}
		})
	}
}

func TestParseTodoListWithFallback(t *testing.T) {
	parser := NewTodoOutputParser()

	tests := []struct {
		name          string
		input         string
		expectedCount int
		expectedTodos []string // descriptions we expect to find
	}{
		{
			name: "robust parsing works - no fallback needed",
			input: `Current todos:
- [todo_1] Fix authentication bug (Status: pending)
- [todo_2] Implement dashboard (Status: in-progress)`,
			expectedCount: 2,
			expectedTodos: []string{"Fix authentication bug", "Implement dashboard"},
		},
		{
			name: "fallback handles malformed input",
			input: `- [todo_1] Simple todo (Status: pending)
- [todo_2] Another todo (Status: completed)`,
			expectedCount: 2,
			expectedTodos: []string{"Simple todo", "Another todo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			todos, err := parser.ParseTodoListWithFallback(tt.input)
			require.NoError(t, err)
			assert.Len(t, todos, tt.expectedCount)

			// Check that expected descriptions are present
			descriptions := make([]string, len(todos))
			for i, todo := range todos {
				descriptions[i] = todo.Description
			}

			for _, expectedDesc := range tt.expectedTodos {
				assert.Contains(t, descriptions, expectedDesc)
			}
		})
	}
}

func TestRenderParsedTodo(t *testing.T) {
	todo := ParsedTodo{
		ID:          "todo_1",
		Description: "Test todo",
		Status:      "pending",
	}

	result := RenderParsedTodo(todo)

	// Should contain the todo description
	assert.Contains(t, result, "Test todo")

	// Should contain some styled content (icon + description)
	assert.Greater(t, len(result), len("Test todo"))
}

func TestParsedTodo_ToTodoType(t *testing.T) {
	parsed := ParsedTodo{
		ID:          "todo_1",
		Description: "Test description",
		Status:      "in-progress",
		RawLine:     "- [todo_1] Test description (Status: in-progress)",
	}

	result := parsed.ToTodoType()

	assert.Equal(t, "todo_1", result.ID)
	assert.Equal(t, "Test description", result.Description)
	assert.Equal(t, "in-progress", result.Status)
}
