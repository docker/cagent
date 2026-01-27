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
	tools.BaseToolSet
	db DB
	// namePrefix, when set, namespaces the tool names to avoid collisions
	// (e.g., "<prefix>_get_memories").
	namePrefix string
}

// Make sure Memory Tool implements the ToolSet Interface
var _ tools.ToolSet = (*MemoryTool)(nil)

func NewMemoryTool(manager DB) *MemoryTool {
	return NewMemoryToolWithPrefix(manager, "")
}

// NewMemoryToolWithPrefix creates a MemoryTool that uses prefixed tool names.
// When prefix is empty, tool names are the legacy unprefixed names.
func NewMemoryToolWithPrefix(manager DB, prefix string) *MemoryTool {
	return &MemoryTool{
		db:         manager,
		namePrefix: prefix,
	}
}

type AddMemoryArgs struct {
	Memory string `json:"memory" jsonschema:"The memory content to store"`
}

type DeleteMemoryArgs struct {
	ID string `json:"id" jsonschema:"The ID of the memory to delete"`
}

func (t *MemoryTool) Instructions() string {
	getMemoriesTool := ToolNameGetMemories
	if t.namePrefix != "" {
		getMemoriesTool = t.namePrefix + "_" + ToolNameGetMemories
	}
	return `## Using the memory tool

Before taking any action or responding to the user use the "` + getMemoriesTool + `" tool to remember things about the user.
Do not talk about using the tool, just use it.

## Rules
- Use the memory tool generously to remember things about the user.`
}

func (t *MemoryTool) toolName(base string) string {
	if t.namePrefix == "" {
		return base
	}
	return t.namePrefix + "_" + base
}

func (t *MemoryTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         t.toolName(ToolNameAddMemory),
			Category:     "memory",
			Description:  "Add a new memory to the database",
			Parameters:   tools.MustSchemaFor[AddMemoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.handleAddMemory),
			Annotations: tools.ToolAnnotations{
				Title: "Add Memory",
			},
		},
		{
			Name:         t.toolName(ToolNameGetMemories),
			Category:     "memory",
			Description:  "Retrieve all stored memories",
			OutputSchema: tools.MustSchemaFor[[]database.UserMemory](),
			Handler:      tools.NewHandler(t.handleGetMemories),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Get Memories",
			},
		},
		{
			Name:         t.toolName(ToolNameDeleteMemory),
			Category:     "memory",
			Description:  "Delete a specific memory by ID",
			Parameters:   tools.MustSchemaFor[DeleteMemoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.handleDeleteMemory),
			Annotations: tools.ToolAnnotations{
				Title: "Delete Memory",
			},
		},
	}, nil
}

func (t *MemoryTool) handleAddMemory(ctx context.Context, args AddMemoryArgs) (*tools.ToolCallResult, error) {
	memory := database.UserMemory{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    args.Memory,
	}

	if err := t.db.AddMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to add memory: %w", err)
	}

	return tools.ResultSuccess(fmt.Sprintf("Memory added successfully with ID: %s", memory.ID)), nil
}

func (t *MemoryTool) handleGetMemories(ctx context.Context, _ map[string]any) (*tools.ToolCallResult, error) {
	memories, err := t.db.GetMemories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get memories: %w", err)
	}

	result, err := json.Marshal(memories)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal memories: %w", err)
	}

	return tools.ResultSuccess(string(result)), nil
}

func (t *MemoryTool) handleDeleteMemory(ctx context.Context, args DeleteMemoryArgs) (*tools.ToolCallResult, error) {
	memory := database.UserMemory{
		ID: args.ID,
	}

	if err := t.db.DeleteMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to delete memory: %w", err)
	}

	return tools.ResultSuccess(fmt.Sprintf("Memory with ID %s deleted successfully", args.ID)), nil
}
