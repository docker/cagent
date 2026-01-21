package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockToolSet struct {
	BaseToolSet
}

func (m *mockToolSet) Tools(_ context.Context) ([]Tool, error) {
	return []Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path",
					},
				},
				"required": []string{"path"},
			},
			Handler: func(_ context.Context, _ ToolCall) (*ToolCallResult, error) {
				return &ToolCallResult{Output: "ok"}, nil
			},
			AddDescriptionParameter: true,
		},
	}, nil
}

func TestDescriptionToolSet_AddsDescriptionParameter(t *testing.T) {
	t.Parallel()
	desc := NewDescriptionToolSet(&mockToolSet{})

	tools, err := desc.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, tools, 1)

	tool := tools[0]
	schema, ok := tool.Parameters.(map[string]any)
	require.True(t, ok)

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	descProp, ok := properties[DescriptionParam].(map[string]any)
	require.True(t, ok, "description parameter should be added")
	assert.Equal(t, "string", descProp["type"])
	assert.Contains(t, descProp["description"], "human-readable")
}

func TestDescriptionToolSet_PreservesOriginalParameters(t *testing.T) {
	t.Parallel()
	desc := NewDescriptionToolSet(&mockToolSet{})

	tools, err := desc.Tools(t.Context())
	require.NoError(t, err)

	schema := tools[0].Parameters.(map[string]any)
	properties := schema["properties"].(map[string]any)

	pathProp, ok := properties["path"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", pathProp["type"])
	assert.Equal(t, "The file path", pathProp["description"])
}

func TestExtractDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     string
		expected string
	}{
		{
			name:     "extracts description",
			args:     `{"path": "/tmp/file.txt", "description": "Reading config file"}`,
			expected: "Reading config file",
		},
		{
			name:     "returns empty for missing description",
			args:     `{"path": "/tmp/file.txt"}`,
			expected: "",
		},
		{
			name:     "returns empty for invalid JSON",
			args:     `invalid json`,
			expected: "",
		},
		{
			name:     "returns empty for non-string description",
			args:     `{"description": 123}`,
			expected: "",
		},
		{
			name:     "handles empty args",
			args:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ExtractDescription(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}
