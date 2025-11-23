package service

import "github.com/docker/cagent/pkg/tui/types"

// TodoManager handles the shared state of all todos
type TodoManager struct {
	todos []types.Todo
}

// NewTodoManager creates a new TodoManager instance
func NewTodoManager() *TodoManager {
	return &TodoManager{
		todos: []types.Todo{},
	}
}

// AddTodo adds a new todo with the given id, description, and status
func (tm *TodoManager) AddTodo(id, description, status string) {
	tm.todos = append(tm.todos, types.Todo{
		ID:          id,
		Description: description,
		Status:      status,
	})
}

// UpdateTodo updates the status of a todo by id
// Returns true if the todo was found and updated, false otherwise
func (tm *TodoManager) UpdateTodo(id, status string) bool {
	for i, todo := range tm.todos {
		if todo.ID == id {
			tm.todos[i].Status = status
			return true
		}
	}
	return false
}

// GetTodos returns all todos
func (tm *TodoManager) GetTodos() []types.Todo {
	return tm.todos
}

// GetTodoByID returns a todo by its ID, or nil if not found
func (tm *TodoManager) GetTodoByID(id string) *types.Todo {
	for _, todo := range tm.todos {
		if todo.ID == id {
			return &todo
		}
	}
	return nil
}
