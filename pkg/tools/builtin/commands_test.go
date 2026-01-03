package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestCommandsTool_Empty(t *testing.T) {
	t.Parallel()

	tool := NewCommandsTool(nil)

	toolList, err := tool.Tools(t.Context())
	require.NoError(t, err)
	assert.Empty(t, toolList)
	assert.Empty(t, tool.Instructions())
}

func TestCommandsTool_Tools(t *testing.T) {
	t.Parallel()

	tool := NewCommandsTool(map[string]string{
		"df": "check disk space",
		"ls": "list files",
	})

	toolList, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolList, 2)

	// Sorted alphabetically
	assert.Equal(t, "command_df", toolList[0].Name)
	assert.Equal(t, "command_ls", toolList[1].Name)

	for _, tl := range toolList {
		assert.Equal(t, "commands", tl.Category)
		assert.True(t, tl.Annotations.ReadOnlyHint)
	}
}

func TestCommandsTool_Handler(t *testing.T) {
	t.Parallel()

	tool := NewCommandsTool(map[string]string{"df": "check disk space"})

	toolList, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolList, 1)

	result, err := toolList[0].Handler(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "check disk space")
}

func TestCommandsTool_Instructions(t *testing.T) {
	t.Parallel()

	tool := NewCommandsTool(map[string]string{
		"df": "check disk space",
		"ls": "list files",
	})

	instructions := tool.Instructions()
	assert.Contains(t, instructions, "Agent Commands")
	assert.Contains(t, instructions, "df")
	assert.Contains(t, instructions, "ls")
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello", truncate("hello", 5))
	assert.Equal(t, "hello...", truncate("hello world", 8))
	assert.Empty(t, truncate("", 10))
}
