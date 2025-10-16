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
	elicitationTool
	manager memorymanager.Manager
}

// Make sure Memory Tool implements the ToolSet Interface
var _ tools.ToolSet = (*MemoryTool)(nil)

func NewMemoryTool(manager memorymanager.Manager) *MemoryTool {
	return &MemoryTool{
		manager: manager,
	}
}

type AddMemoryArgs struct {
	Memory string `json:"memory" jsonschema:"The memory content to store"`
}

type DeleteMemoryArgs struct {
	ID string `json:"id" jsonschema:"The ID of the memory to delete"`
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
			Name:         "add_memory",
			Category:     "memory",
			Description:  "Add a new memory to the database",
			Parameters:   tools.MustSchemaFor[AddMemoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleAddMemory,
			Annotations: tools.ToolAnnotations{
				Title: "Add Memory",
			},
		},
		{
			Name:         "get_memories",
			Category:     "memory",
			Description:  "Retrieve all stored memories",
			OutputSchema: tools.MustSchemaFor[[]database.UserMemory](),
			Handler:      t.handleGetMemories,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Get Memories",
			},
		},
		{
			Name:         "delete_memory",
			Category:     "memory",
			Description:  "Delete a specific memory by ID",
			Parameters:   tools.MustSchemaFor[DeleteMemoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleDeleteMemory,
			Annotations: tools.ToolAnnotations{
				Title: "Delete Memory",
			},
		},
	}, nil
}

func (t *MemoryTool) handleAddMemory(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args AddMemoryArgs
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
	var args DeleteMemoryArgs
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
