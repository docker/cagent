package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

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

	result, err := tool.handler.createTodo(t.Context(), CreateTodoArgs{
		Description: "Test todo item",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created todo [todo_1]: Test todo item")

	assert.Equal(t, 1, tool.handler.todos.Length())
	todos := tool.handler.todos.All()
	require.Len(t, todos, 1)
	assert.Equal(t, "todo_1", todos[0].ID)
	assert.Equal(t, "Test todo item", todos[0].Description)
	assert.Equal(t, "pending", todos[0].Status)

	// Verify Meta contains the created todo
	metaTodos, ok := result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	require.Len(t, metaTodos, 1)
	assert.Equal(t, "todo_1", metaTodos[0].ID)
	assert.Equal(t, "Test todo item", metaTodos[0].Description)
	assert.Equal(t, "pending", metaTodos[0].Status)
}

func TestTodoTool_CreateTodos(t *testing.T) {
	tool := NewTodoTool()

	result, err := tool.handler.createTodos(t.Context(), CreateTodosArgs{
		Descriptions: []string{
			"First todo item",
			"Second todo item",
			"Third todo item",
		},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 3 todos:")
	assert.Contains(t, result.Output, "todo_1")
	assert.Contains(t, result.Output, "todo_2")
	assert.Contains(t, result.Output, "todo_3")

	assert.Equal(t, 3, tool.handler.todos.Length())

	// Verify Meta contains all todos (order not guaranteed from map)
	metaTodos, ok := result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	require.Len(t, metaTodos, 3)

	// Verify order is preserved
	assert.Equal(t, "First todo item", metaTodos[0].Description)
	assert.Equal(t, "Second todo item", metaTodos[1].Description)
	assert.Equal(t, "Third todo item", metaTodos[2].Description)

	result, err = tool.handler.createTodos(t.Context(), CreateTodosArgs{
		Descriptions: []string{
			"Last todo item",
		},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 1 todos:")
	assert.Contains(t, result.Output, "todo_4")
	assert.Equal(t, 4, tool.handler.todos.Length())

	// Verify Meta for second call contains all 4 todos
	metaTodos, ok = result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	require.Len(t, metaTodos, 4)
}

func TestTodoTool_ListTodos(t *testing.T) {
	tool := NewTodoTool()

	todos := []string{
		"First todo item",
		"Second todo item",
		"Third todo item",
	}

	for _, todoDesc := range todos {
		_, err := tool.handler.createTodo(t.Context(), CreateTodoArgs{
			Description: todoDesc,
		})

		require.NoError(t, err)
	}

	result, err := tool.handler.listTodos(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Current todos:")
	for _, todoDesc := range todos {
		assert.Contains(t, result.Output, todoDesc)
		assert.Contains(t, result.Output, "Status: pending")
	}

	// Verify Meta contains all todos
	metaTodos, ok := result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	require.Len(t, metaTodos, 3)
}

func TestTodoTool_UpdateTodos(t *testing.T) {
	tool := NewTodoTool()

	// Create multiple todos first
	_, err := tool.handler.createTodos(t.Context(), CreateTodosArgs{
		Descriptions: []string{
			"First todo item",
			"Second todo item",
			"Third todo item",
		},
	})
	require.NoError(t, err)

	// Update multiple todos in one call
	result, err := tool.handler.updateTodos(t.Context(), UpdateTodosArgs{
		Updates: []TodoUpdate{
			{ID: "todo_1", Status: "completed"},
			{ID: "todo_3", Status: "in-progress"},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Updated 2 todos")
	assert.Contains(t, result.Output, "todo_1 -> completed")
	assert.Contains(t, result.Output, "todo_3 -> in-progress")

	// Verify the todos were updated
	todos := tool.handler.todos.All()
	require.Len(t, todos, 3)
	assert.Equal(t, "completed", todos[0].Status)
	assert.Equal(t, "pending", todos[1].Status)
	assert.Equal(t, "in-progress", todos[2].Status)

	// Verify Meta contains all todos with updated status
	metaTodos, ok := result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	require.Len(t, metaTodos, 3)
}

func TestTodoTool_UpdateTodos_PartialFailure(t *testing.T) {
	tool := NewTodoTool()

	// Create two todos so we can complete one without clearing the list
	_, err := tool.handler.createTodos(t.Context(), CreateTodosArgs{
		Descriptions: []string{"First todo item", "Second todo item"},
	})
	require.NoError(t, err)

	// Try to update one existing and one non-existing todo
	result, err := tool.handler.updateTodos(t.Context(), UpdateTodosArgs{
		Updates: []TodoUpdate{
			{ID: "todo_1", Status: "completed"},
			{ID: "nonexistent", Status: "completed"},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError) // Not an error because at least one succeeded
	assert.Contains(t, result.Output, "Updated 1 todos")
	assert.Contains(t, result.Output, "Not found: nonexistent")

	// Verify the existing todo was updated (list not cleared because todo_2 still pending)
	todos := tool.handler.todos.All()
	require.Len(t, todos, 2)
	assert.Equal(t, "completed", todos[0].Status)
	assert.Equal(t, "pending", todos[1].Status)
}

func TestTodoTool_UpdateTodos_AllNotFound(t *testing.T) {
	tool := NewTodoTool()

	// Try to update non-existing todos
	result, err := tool.handler.updateTodos(t.Context(), UpdateTodosArgs{
		Updates: []TodoUpdate{
			{ID: "nonexistent1", Status: "completed"},
			{ID: "nonexistent2", Status: "completed"},
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError) // Error because all failed
	assert.Contains(t, result.Output, "Not found: nonexistent1, nonexistent2")
}

func TestTodoTool_UpdateTodos_ClearsWhenAllCompleted(t *testing.T) {
	tool := NewTodoTool()

	// Create multiple todos
	_, err := tool.handler.createTodos(t.Context(), CreateTodosArgs{
		Descriptions: []string{"First todo item", "Second todo item"},
	})
	require.NoError(t, err)

	// Complete all todos
	result, err := tool.handler.updateTodos(t.Context(), UpdateTodosArgs{
		Updates: []TodoUpdate{
			{ID: "todo_1", Status: "completed"},
			{ID: "todo_2", Status: "completed"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Updated 2 todos")

	// Verify all todos were cleared
	todos := tool.handler.todos.All()
	assert.Empty(t, todos)

	// Verify Meta is also empty
	metaTodos, ok := result.Meta.([]Todo)
	require.True(t, ok, "Meta should be []Todo")
	assert.Empty(t, metaTodos)
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
