package tools

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

func ToOutputSchemaSchema(valueType reflect.Type) (*jsonschema.Schema, error) {
	return jsonschema.ForType(valueType, &jsonschema.ForOptions{})
}

func ToOutputSchemaSchemaMust(valueType reflect.Type) *jsonschema.Schema {
	schema, err := ToOutputSchemaSchema(valueType)
	if err != nil {
		panic(err)
	}
	return schema
}
