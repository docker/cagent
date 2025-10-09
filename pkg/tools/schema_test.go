package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaFor(t *testing.T, valueType reflect.Type) string {
	t.Helper()

	schema, err := ToOutputSchemaSchema(valueType)
	require.NoError(t, err)

	buf, err := json.Marshal(schema)
	require.NoError(t, err)
	return string(buf)
}

func TestToOutputSchemaSchema_base_types(t *testing.T) {
	assert.JSONEq(t, `{"type":"string"}`, schemaFor(t, reflect.TypeFor[string]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[uint]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[uint8]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[uint16]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[uint32]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[uint64]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[int]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[int8]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[int16]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[int32]()))
	assert.JSONEq(t, `{"type":"integer"}`, schemaFor(t, reflect.TypeFor[int64]()))
	assert.JSONEq(t, `{"type":"number"}`, schemaFor(t, reflect.TypeFor[float32]()))
	assert.JSONEq(t, `{"type":"number"}`, schemaFor(t, reflect.TypeFor[float64]()))
	assert.JSONEq(t, `{"type":"boolean"}`, schemaFor(t, reflect.TypeFor[bool]()))
}

func TestToOutputSchemaSchema_pointers(t *testing.T) {
	assert.JSONEq(t, `{"type":["null","string"]}`, schemaFor(t, reflect.TypeFor[*string]()))
}

func TestToOutputSchemaSchema_base_lists(t *testing.T) {
	assert.JSONEq(t, `{"type":"array","items":{"type":"string"}}`, schemaFor(t, reflect.TypeFor[[]string]()))
	assert.JSONEq(t, `{"type":"array","items":{"type":"integer"}}`, schemaFor(t, reflect.TypeFor[[]int]()))
	assert.JSONEq(t, `{"type":"array","items":{"type":"number"}}`, schemaFor(t, reflect.TypeFor[[]float64]()))
	assert.JSONEq(t, `{"type":"array","items":{"type":"number"}}`, schemaFor(t, reflect.TypeFor[[]float32]()))
	assert.JSONEq(t, `{"type":"array","items":{"type":"boolean"}}`, schemaFor(t, reflect.TypeFor[[]bool]()))
}
