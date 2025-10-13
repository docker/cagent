package tools

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

func MustSchemaFor[T any]() any {
	schema, err := SchemaFor[T]()
	if err != nil {
		panic(err)
	}
	return schema
}

func SchemaFor[T any]() (any, error) {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}
	return schema, nil
}

func SchemaToMap(params any) (map[string]any, error) {
	m := map[string]any{}
	if params != nil {
		buf, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(buf, &m); err != nil {
			return nil, err
		}
	}

	// Ensure we have at least an empty object schema.
	// That's especially important for DMR but can't hurt for others.
	if m["type"] == nil {
		m["type"] = "object"
	}
	if m["properties"] == nil {
		m["properties"] = map[string]any{}
	}

	return m, nil
}

func ConvertSchema(params, v any) error {
	// First unmarshal to a map to check we have a type and non-nil properties
	m, err := SchemaToMap(params)
	if err != nil {
		return err
	}

	// Then another JSON marshal/unmarshal roundtrip to the destination type
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}

	return json.Unmarshal(buf, v)
}
