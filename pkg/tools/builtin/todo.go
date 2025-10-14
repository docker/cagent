package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/tools"
)

type TodoTool struct {
	elicitationTool
	handler *todoHandler
}

// Make sure Todo Tool implements the ToolSet Interface
var _ tools.ToolSet = (*TodoTool)(nil)

type Todo struct {
	ID          string `json:"id" jsonschema:"ID of the todo item"`
	Description string `json:"description" jsonschema:"Description of the todo item"`
	Status      string `json:"status" jsonschema:"New status (pending, in-progress,completed)"`
}

type CreateTodoItem struct {
	Description string `json:"description" jsonschema:"Description of the todo item"`
}

type CreateTodoArgs struct {
	Description string `json:"description" jsonschema:"Description of the todo item"`
}

type CreateTodosArgs struct {
	Todos []CreateTodoItem `json:"todos" jsonschema:"List of todo items"`
}

type UpdateTodoArgs struct {
	ID     string `json:"id" jsonschema:"ID of the todo item"`
	Status string `json:"status" jsonschema:"New status (pending, in-progress,completed)"`
}

type todoHandler struct {
	todos map[string]Todo
}

var NewSharedTodoTool = sync.OnceValue(NewTodoTool)

func NewTodoTool() *TodoTool {
	return &TodoTool{
		handler: &todoHandler{
			todos: make(map[string]Todo),
		},
	}
}

func (t *TodoTool) Instructions() string {
	return `## Using the Todo Tools

IMPORTANT: You MUST use these tools to track the progress of your tasks:

1. Before starting any complex task:
	- Create a todo for each major step using create_todo
	- Break down complex steps into smaller todos

2. While working:
	- Use list_todos frequently to keep track of remaining work
	- Mark todos as "completed" when finished

3. Task Management Rules:
	- Never start a new task without creating a todo for it
	- Always check list_todos before responding to ensure no steps are missed
	- Update todo status to reflect current progress

This toolset is REQUIRED for maintaining task state and ensuring all steps are completed.`
}

func (h *todoHandler) createTodo(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params CreateTodoArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	id := fmt.Sprintf("todo_%d", len(h.todos)+1)
	h.todos[id] = Todo{
		ID:          id,
		Description: params.Description,
		Status:      "pending",
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Created todo %s: %s", id, params.Description),
	}, nil
}

func (h *todoHandler) createTodos(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params CreateTodosArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ids := make([]string, len(params.Todos))
	start := len(h.todos)
	for i, todo := range params.Todos {
		id := fmt.Sprintf("todo_%d", start+i+1)
		h.todos[id] = Todo{
			ID:          id,
			Description: todo.Description,
			Status:      "pending",
		}
		ids[i] = id
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Created %d todos:\n%s", len(params.Todos), strings.Join(ids, "\n")),
	}, nil
}

func (h *todoHandler) updateTodo(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params UpdateTodoArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	todo, exists := h.todos[params.ID]
	if !exists {
		return nil, fmt.Errorf("todo %s not found", params.ID)
	}

	todo.Status = params.Status
	h.todos[params.ID] = todo

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Updated todo %s status to: %s", params.ID, params.Status),
	}, nil
}

func (h *todoHandler) listTodos(context.Context, tools.ToolCall) (*tools.ToolCallResult, error) {
	var output strings.Builder
	output.WriteString("Current todos:\n")

	for _, todo := range h.todos {
		output.WriteString(fmt.Sprintf("- [%s] %s (Status: %s)\n",
			todo.ID, todo.Description, todo.Status))
	}

	return &tools.ToolCallResult{
		Output: output.String(),
	}, nil
}

func (t *TodoTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         "create_todo",
			Description:  "Create a new todo item with a description",
			Parameters:   tools.MustSchemaFor[CreateTodoArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.createTodo,
			Annotations: tools.ToolAnnotations{
				Title:        "Create TODO",
				ReadOnlyHint: true, // Technically not read-only but has practically no destructive side effects.
			},
		},
		{
			Name:         "create_todos",
			Description:  "Create a list of new todo items with descriptions",
			Parameters:   tools.MustSchemaFor[CreateTodosArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.createTodos,
			Annotations: tools.ToolAnnotations{
				Title:        "Create TODOs",
				ReadOnlyHint: true, // Technically not read-only but has practically no destructive side effects.
			},
		},
		{
			Name:         "update_todo",
			Description:  "Update the status of a todo item",
			Parameters:   tools.MustSchemaFor[UpdateTodoArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.updateTodo,
			Annotations: tools.ToolAnnotations{
				Title:        "Update TODO",
				ReadOnlyHint: true, // Technically not read-only but has practically no destructive side effects.
			},
		},
		{
			Name:         "list_todos",
			Description:  "List all current todos with their status",
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.listTodos,
			Annotations: tools.ToolAnnotations{
				Title:        "List TODOs",
				ReadOnlyHint: true,
			},
		},
	}, nil
}

func (t *TodoTool) Start(context.Context) error {
	return nil
}

func (t *TodoTool) Stop() error {
	return nil
}
