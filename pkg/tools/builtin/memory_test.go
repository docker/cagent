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
type MockDB struct {
	mock.Mock
}

func (m *MockDB) AddMemory(ctx context.Context, memory database.UserMemory) error {
	args := m.Called(ctx, memory)
	return args.Error(0)
}

func (m *MockDB) GetMemories(ctx context.Context) ([]database.UserMemory, error) {
	args := m.Called(ctx)
	return args.Get(0).([]database.UserMemory), args.Error(1)
}

func (m *MockDB) DeleteMemory(ctx context.Context, memory database.UserMemory) error {
	args := m.Called(ctx, memory)
	return args.Error(0)
}

func TestMemoryTool_Instructions(t *testing.T) {
	manager := new(MockDB)
	tool := NewMemoryTool(manager)

	instructions := tool.Instructions()
	assert.Contains(t, instructions, "Using the memory tool")
}

func TestMemoryTool_DisplayNames(t *testing.T) {
	manager := new(MockDB)
	tool := NewMemoryTool(manager)

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestMemoryTool_HandleAddMemory(t *testing.T) {
	manager := new(MockDB)
	tool := NewMemoryTool(manager)

	manager.On("AddMemory", mock.Anything, mock.MatchedBy(func(memory database.UserMemory) bool {
		return memory.Memory == "test memory"
	})).Return(nil)

	result, err := tool.handleAddMemory(t.Context(), AddMemoryArgs{
		Memory: "test memory",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Memory added successfully")
	manager.AssertExpectations(t)
}

func TestMemoryTool_HandleGetMemories(t *testing.T) {
	manager := new(MockDB)
	tool := NewMemoryTool(manager)

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

	result, err := tool.handleGetMemories(t.Context(), nil)
	require.NoError(t, err)

	var returnedMemories []database.UserMemory
	err = json.Unmarshal([]byte(result.Output), &returnedMemories)
	require.NoError(t, err)

	assert.Len(t, returnedMemories, 2)
	assert.Equal(t, memories, returnedMemories)
	manager.AssertExpectations(t)
}

func TestMemoryTool_HandleDeleteMemory(t *testing.T) {
	manager := new(MockDB)
	tool := NewMemoryTool(manager)

	manager.On("DeleteMemory", mock.Anything, mock.MatchedBy(func(memory database.UserMemory) bool {
		return memory.ID == "1"
	})).Return(nil)

	result, err := tool.handleDeleteMemory(t.Context(), DeleteMemoryArgs{
		ID: "1",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Memory with ID 1 deleted successfully")
	manager.AssertExpectations(t)
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

func TestMemoryTool_WithPrefix_NamespacesToolNames(t *testing.T) {
	t.Parallel()

	manager := new(MockDB)
	toolset := NewMemoryToolWithPrefix(manager, "longterm")

	all, err := toolset.Tools(t.Context())
	require.NoError(t, err)

	names := make([]string, 0, len(all))
	for _, tl := range all {
		names = append(names, tl.Name)
	}

	assert.Contains(t, names, "longterm_"+ToolNameAddMemory)
	assert.Contains(t, names, "longterm_"+ToolNameGetMemories)
	assert.Contains(t, names, "longterm_"+ToolNameDeleteMemory)
}
