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
	assert.Equal(t, "create_todo", allTools[0].Function.Name)
	assert.Equal(t, "create_todos", allTools[1].Function.Name)
	assert.Equal(t, "update_todo", allTools[2].Function.Name)
	assert.Equal(t, "list_todos", allTools[3].Function.Name)

	// Check create_todo parameters
	createProps := allTools[0].Function.Parameters.Properties
	assert.Contains(t, createProps, "description")
	assert.Contains(t, allTools[0].Function.Parameters.Required, "description")

	// Check update_todo parameters
	updateProps := allTools[2].Function.Parameters.Properties
	assert.Contains(t, updateProps, "id")
	assert.Contains(t, updateProps, "status")
	assert.Contains(t, allTools[2].Function.Parameters.Required, "id")
	assert.Contains(t, allTools[2].Function.Parameters.Required, "status")

	// Verify handlers are provided
	assert.NotNil(t, allTools[0].Handler)
	assert.NotNil(t, allTools[1].Handler)
	assert.NotNil(t, allTools[2].Handler)
	assert.NotNil(t, allTools[3].Handler)
}

func TestTodoTool_DisplayNames(t *testing.T) {
	tool := NewTodoTool()

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Function.Name, tool.DisplayName())
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
	args := Todo{
		Description: "Test todo item",
	}
	argsBytes, _ := json.Marshal(args)

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
	args := struct {
		Todos []Todo `json:"todos"`
	}{
		Todos: []Todo{
			{
				Description: "First todo item",
			},
			{
				Description: "Second todo item",
			},
			{
				Description: "Third todo item",
			},
		},
	}
	argsBytes, _ := json.Marshal(args)

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
	args = struct {
		Todos []Todo `json:"todos"`
	}{
		Todos: []Todo{
			{
				Description: "Last todo item",
			},
		},
	}
	argsBytes, _ = json.Marshal(args)

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
	createArgs := Todo{
		Description: "Test todo item",
	}
	createArgsBytes, _ := json.Marshal(createArgs)

	createToolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "create_todo",
			Arguments: string(createArgsBytes),
		},
	}

	_, err = createHandler(t.Context(), createToolCall)
	require.NoError(t, err)

	// Now update it
	updateArgs := struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}{
		ID:     "todo_1",
		Status: "completed",
	}
	updateArgsBytes, _ := json.Marshal(updateArgs)

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
		createArgs := Todo{
			Description: todoDesc,
		}
		createArgsBytes, _ := json.Marshal(createArgs)
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
	updateArgs := struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}{
		ID:     "nonexistent_todo",
		Status: "completed",
	}
	updateArgsBytes, _ := json.Marshal(updateArgs)

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
