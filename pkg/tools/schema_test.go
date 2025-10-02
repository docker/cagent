package tools

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/memory/database"
)

func TestToOutputSchemaSchema_base_types(t *testing.T) {
	assert.Equal(t, ToolOutputSchema{Type: "string"}, ToOutputSchemaSchema(reflect.TypeFor[string]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[uint]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[uint8]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[uint16]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[uint32]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[uint64]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[int]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[int8]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[int16]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[int32]()))
	assert.Equal(t, ToolOutputSchema{Type: "int"}, ToOutputSchemaSchema(reflect.TypeFor[int64]()))
	assert.Equal(t, ToolOutputSchema{Type: "number"}, ToOutputSchemaSchema(reflect.TypeFor[float32]()))
	assert.Equal(t, ToolOutputSchema{Type: "number"}, ToOutputSchemaSchema(reflect.TypeFor[float64]()))
	assert.Equal(t, ToolOutputSchema{Type: "boolean"}, ToOutputSchemaSchema(reflect.TypeFor[bool]()))
}

func TestToOutputSchemaSchema_pointers(t *testing.T) {
	assert.Equal(t, ToolOutputSchema{Type: "string"}, ToOutputSchemaSchema(reflect.TypeFor[*string]()))
}

func TestToOutputSchemaSchema_base_lists(t *testing.T) {
	assert.Equal(t, ToolOutputSchema{Type: "array", Items: map[string]any{"type": "string"}}, ToOutputSchemaSchema(reflect.TypeFor[[]string]()))
	assert.Equal(t, ToolOutputSchema{Type: "array", Items: map[string]any{"type": "int"}}, ToOutputSchemaSchema(reflect.TypeFor[[]int]()))
	assert.Equal(t, ToolOutputSchema{Type: "array", Items: map[string]any{"type": "number"}}, ToOutputSchemaSchema(reflect.TypeFor[[]float64]()))
	assert.Equal(t, ToolOutputSchema{Type: "array", Items: map[string]any{"type": "number"}}, ToOutputSchemaSchema(reflect.TypeFor[[]float32]()))
	assert.Equal(t, ToolOutputSchema{Type: "array", Items: map[string]any{"type": "boolean"}}, ToOutputSchemaSchema(reflect.TypeFor[[]bool]()))
}

func TestToOutputSchemaSchema_memories(t *testing.T) {
	outputScheme := ToOutputSchemaSchema(reflect.TypeFor[[]database.UserMemory]())

	expected := ToolOutputSchema{
		Type: "array",
		Items: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ID": map[string]any{
					"type":        "string",
					"description": "The ID of the memory",
				},
				"CreatedAt": map[string]any{
					"type":        "string",
					"description": "The creation timestamp of the memory",
				},
				"Memory": map[string]any{
					"type":        "string",
					"description": "The content of the memory",
				},
			},
		},
	}

	assert.Equal(t, expected, outputScheme)
}

func TestToOutputSchemaSchema_recursive(t *testing.T) {
	type TreeNode struct {
		ID       string    `description:"The ID"`
		Children *TreeNode `description:"The child nodes"`
	}

	outputScheme := ToOutputSchemaSchema(reflect.TypeFor[[]TreeNode]())

	expected := ToolOutputSchema{
		Type: "array",
		Items: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ID": map[string]any{
					"type":        "string",
					"description": "The ID",
				},
				"Children": map[string]any{
					"$ref":        "#",
					"description": "The child nodes",
				},
			},
		},
	}

	assert.Equal(t, expected, outputScheme)
}

func TestToOutputSchemaSchema_array(t *testing.T) {
	type TreeNode struct {
		Values []string `description:"The values"`
	}

	outputScheme := ToOutputSchemaSchema(reflect.TypeFor[[]TreeNode]())

	expected := ToolOutputSchema{
		Type: "array",
		Items: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Values": map[string]any{
					"type":        "array",
					"description": "The values",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	assert.Equal(t, expected, outputScheme)
}

func TestToOutputSchemaSchema_array_json(t *testing.T) {
	type TreeNode struct {
		Values []string `json:"values" description:"The values"`
	}

	outputScheme := ToOutputSchemaSchema(reflect.TypeFor[[]TreeNode]())

	expected := ToolOutputSchema{
		Type: "array",
		Items: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"values": map[string]any{
					"type":        "array",
					"description": "The values",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	assert.Equal(t, expected, outputScheme)
}
