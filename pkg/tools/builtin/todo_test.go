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
	assert.Zero(t, tool.handler.todos.Length())
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
	for _, tool := range allTools {
		assert.NotNil(t, tool.Handler)
		assert.Equal(t, "todo", tool.Category)
	}

	assert.Equal(t, "create_todo", allTools[0].Name)
	assert.Equal(t, "create_todos", allTools[1].Name)
	assert.Equal(t, "update_todo", allTools[2].Name)
	assert.Equal(t, "list_todos", allTools[3].Name)

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

	schema, err = json.Marshal(allTools[1].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"required": [
		"descriptions"
	],
	"properties": {
		"descriptions": {
			"type": "array",
			"description": "Descriptions of the todo items",
			"items": {
				"type": "string"
			}
		}
	},
	"additionalProperties": false
}`, string(schema))

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

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler

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

	result, err := createHandler(t.Context(), toolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created todo [todo_1]: Test todo item")

	assert.Equal(t, 1, tool.handler.todos.Length())
	todo, exists := tool.handler.todos.Load("todo_1")
	assert.True(t, exists)
	assert.Equal(t, "Test todo item", todo.Description)
	assert.Equal(t, "pending", todo.Status)
}

func TestTodoTool_CreateTodos(t *testing.T) {
	tool := NewTodoTool()

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createTodosHandler := tls[1].Handler

	args := CreateTodosArgs{
		Descriptions: []string{
			"First todo item",
			"Second todo item",
			"Third todo item",
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

	result, err := createTodosHandler(t.Context(), toolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 3 todos:")
	assert.Contains(t, result.Output, "todo_1")
	assert.Contains(t, result.Output, "todo_2")
	assert.Contains(t, result.Output, "todo_3")

	assert.Equal(t, 3, tool.handler.todos.Length())

	args = CreateTodosArgs{
		Descriptions: []string{
			"Last todo item",
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

	result, err = createTodosHandler(t.Context(), toolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 1 todos:")
	assert.Contains(t, result.Output, "todo_4")
	assert.Equal(t, 4, tool.handler.todos.Length())
}

func TestTodoTool_UpdateTodo(t *testing.T) {
	tool := NewTodoTool()

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

	result, err := updateHandler(t.Context(), updateToolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Updated todo [todo_1] to status: [completed]")

	todo, exists := tool.handler.todos.Load("todo_1")
	assert.True(t, exists)
	assert.Equal(t, "completed", todo.Status)
}

func TestTodoTool_ListTodos(t *testing.T) {
	tool := NewTodoTool()

	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tls, 4)

	createHandler := tls[0].Handler
	listHandler := tls[3].Handler

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

	result, err := listHandler(t.Context(), listToolCall)

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Current todos:")
	for _, todoDesc := range todos {
		assert.Contains(t, result.Output, todoDesc)
		assert.Contains(t, result.Output, "Status: pending")
	}
}

func TestTodoTool_UpdateNonexistentTodo(t *testing.T) {
	tool := NewTodoTool()

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

	_, err = updateHandler(t.Context(), updateToolCall)

	assert.ErrorContains(t, err, "not found")
}

func TestTodoTool_InvalidArguments(t *testing.T) {
	tool := NewTodoTool()

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

	err := tool.Start(t.Context())
	require.NoError(t, err)

	err = tool.Stop(t.Context())
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
