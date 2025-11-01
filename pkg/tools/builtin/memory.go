package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/cagent/pkg/memory/database"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameAddMemory    = "add_memory"
	ToolNameGetMemories  = "get_memories"
	ToolNameDeleteMemory = "delete_memory"
)

type DB interface {
	AddMemory(ctx context.Context, memory database.UserMemory) error
	GetMemories(ctx context.Context) ([]database.UserMemory, error)
	DeleteMemory(ctx context.Context, memory database.UserMemory) error
}

type MemoryTool struct {
	tools.ElicitationTool
	db DB
}

// Make sure Memory Tool implements the ToolSet Interface
var _ tools.ToolSet = (*MemoryTool)(nil)

func NewMemoryTool(manager DB) *MemoryTool {
	return &MemoryTool{
		db: manager,
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
			Name:         ToolNameAddMemory,
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
			Name:         ToolNameGetMemories,
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
			Name:         ToolNameDeleteMemory,
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

	if err := t.db.AddMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to add memory: %w", err)
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Memory added successfully with ID: %s", memory.ID),
	}, nil
}

func (t *MemoryTool) handleGetMemories(ctx context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	memories, err := t.db.GetMemories(ctx)
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

	if err := t.db.DeleteMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to delete memory: %w", err)
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Memory with ID %s deleted successfully", args.ID),
	}, nil
}

func (t *MemoryTool) Start(context.Context) error {
	return nil
}

func (t *MemoryTool) Stop(context.Context) error {
	return nil
}
