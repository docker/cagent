package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/cagent/pkg/memory/database"
	"github.com/docker/cagent/pkg/memorymanager"
	"github.com/docker/cagent/pkg/tools"
)

type MemoryTool struct {
	manager memorymanager.Manager
}

// Make sure Memory Tool implements the ToolSet Interface
var _ tools.ToolSet = (*MemoryTool)(nil)

func NewMemoryTool(manager memorymanager.Manager) *MemoryTool {
	return &MemoryTool{
		manager: manager,
	}
}

func (t *MemoryTool) Instructions() string {
	return `## Using the memory tool

Before taking any action or responding to the user use the "get_memories" tool to remember things about the user.
Do not talk about using the tool, just use it.

## Rules
- Use the memory tool generously to remember things about the user.`
}

func (t *MemoryTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Function: &tools.FunctionDefinition{
				Name:        "add_memory",
				Description: "Add a new memory to the database",
				Annotations: tools.ToolAnnotation{
					Title: "Add Memory",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"memory": map[string]any{
							"type":        "string",
							"description": "The memory content to store",
						},
					},
					Required: []string{"memory"},
				},
			},
			Handler: t.handleAddMemory,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "get_memories",
				Description: "Retrieve all stored memories",
				Annotations: tools.ToolAnnotation{
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Get Memories",
				},
			},
			Handler: t.handleGetMemories,
		},
		{
			Function: &tools.FunctionDefinition{
				Name:        "delete_memory",
				Description: "Delete a specific memory by ID",
				Annotations: tools.ToolAnnotation{
					Title: "Delete Memory",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "The ID of the memory to delete",
						},
					},
					Required: []string{"id"},
				},
			},
			Handler: t.handleDeleteMemory,
		},
	}, nil
}

func (t *MemoryTool) handleAddMemory(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args struct {
		Memory string `json:"memory"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	memory := database.UserMemory{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    args.Memory,
	}

	if err := t.manager.AddMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to add memory: %w", err)
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Memory added successfully with ID: %s", memory.ID),
	}, nil
}

func (t *MemoryTool) handleGetMemories(ctx context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	memories, err := t.manager.GetMemories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get memories: %w", err)
	}

	result, err := json.Marshal(memories)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal memories: %w", err)
	}

	return &tools.ToolCallResult{
		Output: string(result),
	}, nil
}

func (t *MemoryTool) handleDeleteMemory(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	memory := database.UserMemory{
		ID: args.ID,
	}

	if err := t.manager.DeleteMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to delete memory: %w", err)
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Memory with ID %s deleted successfully", args.ID),
	}, nil
}

func (t *MemoryTool) Start(context.Context) error {
	return nil
}

func (t *MemoryTool) Stop() error {
	return nil
}
