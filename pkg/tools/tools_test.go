package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionParameters_Marshal(t *testing.T) {
	fp := FunctionParameters{
		Type: "object",
		Properties: map[string]any{
			"foo": map[string]any{
				"type":        "string",
				"description": "A foo string",
			},
			"bar": map[string]any{
				"type": "integer",
			},
		},
		Required: []string{"foo"},
	}

	data, err := json.Marshal(fp)
	require.NoError(t, err)

	expected := `{"type":"object","properties":{"bar":{"type":"integer"},"foo":{"description":"A foo string","type":"string"}},"required":["foo"]}`
	require.Equal(t, expected, string(data))
}

func TestFunctionParameters_Marshal_MissingType(t *testing.T) {
	fp := FunctionParameters{
		Properties: map[string]any{
			"foo": map[string]any{
				"type": "string",
			},
		},
	}

	data, err := json.Marshal(fp)
	require.NoError(t, err)

	expected := `{"type":"object","properties":{"foo":{"type":"string"}}}`
	require.Equal(t, expected, string(data))

	// Make sure the original struct is not modified
	assert.Empty(t, fp.Type)
}

// TestFunctionParameters_MarshalEmpty makes sure we format empty properties in a way that
// OpenAI and LM Studio accept.
// See https://github.com/docker/cagent/issues/278
func TestFunctionParameters_Marshal_Empty(t *testing.T) {
	fp := FunctionParameters{}

	data, err := json.Marshal(fp)
	require.NoError(t, err)

	expected := `{"type":"object","properties":{}}`
	require.Equal(t, expected, string(data))

	// Make sure the original struct is not modified
	assert.Empty(t, fp.Type)
	assert.Nil(t, fp.Properties)
}
