package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaToMap_Nil(t *testing.T) {
	m, err := SchemaToMap(nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, m)
}

func TestSchemaToMap_MissingType(t *testing.T) {
	m, err := SchemaToMap(map[string]any{
		"properties": map[string]any{},
	})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, m)
}

func TestSchemaToMap_MissingEmptyProperties(t *testing.T) {
	m, err := SchemaToMap(map[string]any{
		"type": "object",
	})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, m)
}
