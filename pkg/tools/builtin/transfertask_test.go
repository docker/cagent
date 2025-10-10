package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTaskTool(t *testing.T) {
	tool := NewTransferTaskTool()
	assert.NotNil(t, tool)
}

func TestTaskTool_Instructions(t *testing.T) {
	tool := NewTransferTaskTool()
	instructions := tool.Instructions()
	assert.Empty(t, instructions)
}

func TestTaskTool_Tools(t *testing.T) {
	tool := NewTransferTaskTool()

	allTools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, allTools, 1)

	// Verify transfer_task function
	assert.Equal(t, "transfer_task", allTools[0].Name)
	assert.Contains(t, allTools[0].Description, "transfer a task to the selected team member")

	// Check parameters
	props := allTools[0].Parameters.Properties
	assert.Contains(t, props, "agent")
	assert.Contains(t, props, "task")

	// Check required fields
	assert.Contains(t, allTools[0].Parameters.Required, "agent")
	assert.Contains(t, allTools[0].Parameters.Required, "task")

	// Verify no handler is provided (it's handled externally)
	assert.Nil(t, allTools[0].Handler)
}

func TestTaskTool_DisplayNames(t *testing.T) {
	tool := NewTransferTaskTool()

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestTaskTool_StartStop(t *testing.T) {
	tool := NewTransferTaskTool()

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop()
	require.NoError(t, err)
}
