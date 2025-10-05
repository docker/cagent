package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

type TodoTool struct {
	elicitationTool
	handler *todoHandler
}

// Make sure Todo Tool implements the ToolSet Interface
var _ tools.ToolSet = (*TodoTool)(nil)

type Todo struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending", "completed"
}

type todoHandler struct {
	todos map[string]Todo
}

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
	var params struct {
		Description string `json:"description"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	id := fmt.Sprintf("todo_%d", len(h.todos)+1)
	todo := Todo{
		ID:          id,
		Description: params.Description,
		Status:      "pending",
	}
	h.todos[id] = todo

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Created todo %s: %s", id, params.Description),
	}, nil
}

func (h *todoHandler) createTodos(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params struct {
		Todos []Todo `json:"todos"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ids := make([]string, len(params.Todos))
	start := len(h.todos)
	for i, todo := range params.Todos {
		id := fmt.Sprintf("todo_%d", start+i+1)
		todo := Todo{
			ID:          id,
			Description: todo.Description,
			Status:      "pending",
		}

		h.todos[id] = todo
		ids[i] = id
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Created %d todos:\n%s", len(params.Todos), strings.Join(ids, "\n")),
	}, nil
}

func (h *todoHandler) updateTodo(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

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
			Function: &tools.FunctionDefinition{
				Name:        "create_todo",
				Description: "Create a new todo item with a description",
				Annotations: tools.ToolAnnotation{
					// This is technically not read-only but has practically no destructive side effects.
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Create TODO",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"description": map[string]any{
							"type":        "string",
							"description": "Description of the todo item",
						},
					},
					Required: []string{"description"},
				},
				OutputSchema: tools.ToOutputSchemaSchema(reflect.TypeFor[string]()),
			},
			Handler: t.handler.createTodo,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "create_todos",
				Description: "Create a list of new todo items with descriptions",
				Annotations: tools.ToolAnnotation{
					// This is technically not read-only but has practically no destructive side effects.
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Create TODOs",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"todos": map[string]any{
							"type":        "array",
							"description": "List of todo items",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"description": map[string]any{
										"type":        "string",
										"description": "Description of the todo item",
									},
								},
							},
						},
					},
					Required: []string{"todos"},
				},
				OutputSchema: tools.ToOutputSchemaSchema(reflect.TypeFor[string]()),
			},
			Handler: t.handler.createTodos,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "update_todo",
				Description: "Update the status of a todo item",
				Annotations: tools.ToolAnnotation{
					// This is technically not read-only but has practically no destructive side effects.
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Update TODO",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "ID of the todo item",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "New status (pending, in-progress,completed)",
						},
					},
					Required: []string{"id", "status"},
				},
				OutputSchema: tools.ToOutputSchemaSchema(reflect.TypeFor[string]()),
			},
			Handler: t.handler.updateTodo,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "list_todos",
				Description: "List all current todos with their status",
				Annotations: tools.ToolAnnotation{
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "List TODOs",
				},
				OutputSchema: tools.ToOutputSchemaSchema(reflect.TypeFor[string]()),
			},
			Handler: t.handler.listTodos,
		},
	}, nil
}

func (t *TodoTool) Start(context.Context) error {
	return nil
}

func (t *TodoTool) Stop() error {
	return nil
}
