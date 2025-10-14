package builtin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewTodoTool(t *testing.T) {
	tool := NewTodoTool()

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Empty(t, tool.handler.todos)
}

func TestTodoTool_Instructions(t *testing.T) {
	tool := NewTodoTool()

	instructions := tool.Instructions()
	assert.Contains(t, instructions, "Using the Todo Tools")
	assert.Contains(t, instructions, "Create a todo for each major step")
	assert.Contains(t, instructions, "This toolset is REQUIRED")
}

func TestTodoTool_Tools(t *testing.T) {
	tool := NewTodoTool()

	allTools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, allTools, 4)

	// Verify tool functions
	assert.Equal(t, "create_todo", allTools[0].Name)
	assert.Equal(t, "create_todos", allTools[1].Name)
	assert.Equal(t, "update_todo", allTools[2].Name)
	assert.Equal(t, "list_todos", allTools[3].Name)
	assert.NotNil(t, allTools[0].Handler)
	assert.NotNil(t, allTools[1].Handler)
	assert.NotNil(t, allTools[2].Handler)
	assert.NotNil(t, allTools[3].Handler)

	// Check create_todo parameters
	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"description": {
			"description": "Description of the todo item",
			"type": "string"
		}
	},
	"additionalProperties": false,
	"required": [
		"description"
	]
}`, string(schema))

	// Check create_todos parameters
	schema, err = json.Marshal(allTools[1].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"required": [
		"todos"
	],
	"properties": {
		"todos": {
			"type": "array",
			"description": "List of todo items",
			"items": {
				"type": "object",
				"required": [
					"id",
					"description",
					"status"
				],
				"properties": {
					"description": {
						"type": "string",
						"description": "Description of the todo item"
					},
					"id": {
						"type": "string",
						"description": "ID of the todo item"
					},
					"status": {
						"type": "string",
						"description": "New status (pending, in-progress,completed)"
					}
				},
				"additionalProperties": false
			}
		}
	},
	"additionalProperties": false
}`, string(schema))

	// Check update_todo parameters
	schema, err = json.Marshal(allTools[2].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"id": {
			"description": "ID of the todo item",
			"type": "string"
		},
		"status": {
			"description": "New status (pending, in-progress,completed)",
			"type": "string"
		}
	},
	"additionalProperties": false,
	"required": [
		"id",
		"status"
	]
}`, string(schema))

	// Check list_todos parameters
	assert.Nil(t, allTools[3].Parameters)
}

func TestTodoTool_DisplayNames(t *testing.T) {
	tool := NewTodoTool()

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestTodoTool_CreateTodo(t *testing.T) {
	tool := NewTodoTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler

	// Create tool call
	args := CreateTodoArgs{
		Description: "Test todo item",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todo",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := createHandler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created todo todo_1: Test todo item")

	// Verify todo was added to the handler's todos map
	assert.Len(t, tool.handler.todos, 1)
	todo, exists := tool.handler.todos["todo_1"]
	assert.True(t, exists)
	assert.Equal(t, "Test todo item", todo.Description)
	assert.Equal(t, "pending", todo.Status)
}

func TestTodoTool_CreateTodos(t *testing.T) {
	tool := NewTodoTool()

	// Get handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createTodosHandler := tls[1].Handler

	// Create multiple todos
	args := CreateTodosArgs{
		Todos: []Todo{
			{Description: "First todo item"},
			{Description: "Second todo item"},
			{Description: "Third todo item"},
		},
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todos",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := createTodosHandler(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 3 todos:")
	assert.Contains(t, result.Output, "todo_1")
	assert.Contains(t, result.Output, "todo_2")
	assert.Contains(t, result.Output, "todo_3")

	// Verify todos were added to the handler's todos map
	assert.Len(t, tool.handler.todos, 3)

	// Create multiple todos
	args = CreateTodosArgs{
		Todos: []Todo{
			{Description: "Last todo item"},
		},
	}
	argsBytes, err = json.Marshal(args)
	require.NoError(t, err)

	toolCall = tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todos",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err = createTodosHandler(t.Context(), toolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 1 todos:")
	assert.Contains(t, result.Output, "todo_4")
	assert.Len(t, tool.handler.todos, 4)
}

func TestTodoTool_UpdateTodo(t *testing.T) {
	tool := NewTodoTool()

	// Get handlers from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler
	updateHandler := tls[2].Handler

	// First create a todo
	createArgs := CreateTodoArgs{
		Description: "Test todo item",
	}
	createArgsBytes, err := json.Marshal(createArgs)
	require.NoError(t, err)

	createToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todo",
			Arguments: string(createArgsBytes),
		},
	}

	_, err = createHandler(t.Context(), createToolCall)
	require.NoError(t, err)

	// Now update it
	updateArgs := UpdateTodoArgs{
		ID:     "todo_1",
		Status: "completed",
	}
	updateArgsBytes, err := json.Marshal(updateArgs)
	require.NoError(t, err)

	updateToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "update_todo",
			Arguments: string(updateArgsBytes),
		},
	}

	// Call update handler
	result, err := updateHandler(t.Context(), updateToolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Updated todo todo_1 status to: completed")

	// Verify todo status was updated
	todo, exists := tool.handler.todos["todo_1"]
	assert.True(t, exists)
	assert.Equal(t, "completed", todo.Status)
}

func TestTodoTool_ListTodos(t *testing.T) {
	tool := NewTodoTool()

	// Get handlers from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler
	listHandler := tls[3].Handler

	// Create a few todos
	todos := []string{
		"First todo item",
		"Second todo item",
		"Third todo item",
	}

	for _, todoDesc := range todos {
		createArgs := CreateTodoArgs{
			Description: todoDesc,
		}
		createArgsBytes, err := json.Marshal(createArgs)
		require.NoError(t, err)

		createToolCall := tools.ToolCall{
			Function: tools.FunctionCall{
				Name:      "create_todo",
				Arguments: string(createArgsBytes),
			},
		}

		_, err = createHandler(t.Context(), createToolCall)
		require.NoError(t, err)
	}

	// Now list them
	listToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "list_todos",
			Arguments: "{}",
		},
	}

	// Call list handler
	result, err := listHandler(t.Context(), listToolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Current todos:")
	for _, todoDesc := range todos {
		assert.Contains(t, result.Output, todoDesc)
		assert.Contains(t, result.Output, "Status: pending")
	}
}

func TestTodoTool_UpdateNonexistentTodo(t *testing.T) {
	tool := NewTodoTool()

	// Get update handler from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	updateHandler := tls[2].Handler

	// Try to update a non-existent todo
	updateArgs := UpdateTodoArgs{
		ID:     "nonexistent_todo",
		Status: "completed",
	}
	updateArgsBytes, err := json.Marshal(updateArgs)
	require.NoError(t, err)

	updateToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "update_todo",
			Arguments: string(updateArgsBytes),
		},
	}

	// Call update handler
	_, err = updateHandler(t.Context(), updateToolCall)

	// Verify error
	assert.ErrorContains(t, err, "not found")
}

func TestTodoTool_InvalidArguments(t *testing.T) {
	tool := NewTodoTool()

	// Get handlers from tool
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler
	updateHandler := tls[2].Handler

	// Invalid JSON for create_todo
	createToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todo",
			Arguments: "{invalid json",
		},
	}

	_, err = createHandler(t.Context(), createToolCall)
	require.Error(t, err)

	// Invalid JSON for update_todo
	updateToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "update_todo",
			Arguments: "{invalid json",
		},
	}

	_, err = updateHandler(t.Context(), updateToolCall)
	require.Error(t, err)
}

func TestTodoTool_StartStop(t *testing.T) {
	tool := NewTodoTool()

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop()
	require.NoError(t, err)
}

func TestTodoTool_OutputSchema(t *testing.T) {
	tool := NewTodoTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestTodoTool_ParametersAreObjects(t *testing.T) {
	tool := NewTodoTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
