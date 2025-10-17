package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/memory/database"
	"github.com/docker/cagent/pkg/tools"
)

// Mock for memorymanager.Manager
type MockMemoryManager struct {
	mock.Mock
}

func (m *MockMemoryManager) AddMemory(ctx context.Context, memory database.UserMemory) error {
	args := m.Called(ctx, memory)
	return args.Error(0)
}

func (m *MockMemoryManager) GetMemories(ctx context.Context) ([]database.UserMemory, error) {
	args := m.Called(ctx)
	return args.Get(0).([]database.UserMemory), args.Error(1)
}

func (m *MockMemoryManager) DeleteMemory(ctx context.Context, memory database.UserMemory) error {
	args := m.Called(ctx, memory)
	return args.Error(0)
}

func TestNewMemoryTool(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	assert.NotNil(t, tool)
	assert.Equal(t, manager, tool.manager)
}

func TestMemoryTool_Instructions(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	instructions := tool.Instructions()
	assert.Contains(t, instructions, "Using the memory tool")
}

func TestMemoryTool_Tools(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 3)
	for _, tool := range allTools {
		assert.NotNil(t, tool.Handler)
		assert.Equal(t, "memory", tool.Category)
	}

	// Verify tool functions
	assert.Equal(t, "add_memory", allTools[0].Name)
	assert.Equal(t, "get_memories", allTools[1].Name)
	assert.Equal(t, "delete_memory", allTools[2].Name)

	// Check add_memory parameters
	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"memory": {
			"description": "The memory content to store",
			"type": "string"
		}
	},
	"additionalProperties": false,
	"required": [
		"memory"
	]
}`, string(schema))

	// Check get_memories parameters
	assert.Nil(t, allTools[1].Parameters)

	// Check delete_memory parameters
	schema, err = json.Marshal(allTools[2].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"id": {
			"description": "The ID of the memory to delete",
			"type": "string"
		}
	},
	"additionalProperties": false,
	"required": [
		"id"
	]
}`, string(schema))
}

func TestMemoryTool_DisplayNames(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestMemoryTool_HandleAddMemory(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	// Setup mock using database.UserMemory
	manager.On("AddMemory", mock.Anything, mock.MatchedBy(func(memory database.UserMemory) bool {
		return memory.Memory == "test memory"
	})).Return(nil)

	// Create tool call
	args := AddMemoryArgs{
		Memory: "test memory",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "add_memory",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := tool.handleAddMemory(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Memory added successfully")
	manager.AssertExpectations(t)
}

func TestMemoryTool_HandleGetMemories(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	// Setup mock using database.UserMemory
	memories := []database.UserMemory{
		{
			ID:        "1",
			CreatedAt: time.Now().Format(time.RFC3339),
			Memory:    "memory 1",
		},
		{
			ID:        "2",
			CreatedAt: time.Now().Format(time.RFC3339),
			Memory:    "memory 2",
		},
	}
	manager.On("GetMemories", mock.Anything).Return(memories, nil)

	// Create tool call
	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "get_memories",
			Arguments: "{}",
		},
	}

	// Call handler
	result, err := tool.handleGetMemories(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)

	var returnedMemories []database.UserMemory
	err = json.Unmarshal([]byte(result.Output), &returnedMemories)
	require.NoError(t, err)

	assert.Len(t, returnedMemories, 2)
	assert.Equal(t, memories, returnedMemories)
	manager.AssertExpectations(t)
}

func TestMemoryTool_HandleDeleteMemory(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	// Setup mock using database.UserMemory
	manager.On("DeleteMemory", mock.Anything, mock.MatchedBy(func(memory database.UserMemory) bool {
		return memory.ID == "1"
	})).Return(nil)

	// Create tool call
	args := DeleteMemoryArgs{
		ID: "1",
	}
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "delete_memory",
			Arguments: string(argsBytes),
		},
	}

	// Call handler
	result, err := tool.handleDeleteMemory(t.Context(), toolCall)

	// Verify
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Memory with ID 1 deleted successfully")
	manager.AssertExpectations(t)
}

func TestMemoryTool_InvalidArguments(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	// Invalid JSON for add_memory
	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "add_memory",
			Arguments: "{invalid json",
		},
	}

	result, err := tool.handleAddMemory(t.Context(), toolCall)
	require.Error(t, err)
	assert.Nil(t, result)

	// Invalid JSON for delete_memory
	toolCall = tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "delete_memory",
			Arguments: "{invalid json",
		},
	}

	result, err = tool.handleDeleteMemory(t.Context(), toolCall)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestMemoryTool_StartStop(t *testing.T) {
	manager := new(MockMemoryManager)
	tool := NewMemoryTool(manager)

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop(t.Context())
	require.NoError(t, err)
}

func TestMemoryTool_OutputSchema(t *testing.T) {
	tool := NewMemoryTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestMemoryTool_ParametersAreObjects(t *testing.T) {
	tool := NewMemoryTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
