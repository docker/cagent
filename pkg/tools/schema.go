package tools

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

func MustSchemaFor[T any]() any {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{})
	if err != nil {
		panic(err)
	}
	return schema
}

func ConvertSchema(params, v any) error {
	// First unmarshal to a map to check we have a type and non-nil properties
	m := map[string]any{}
	if params != nil {
		buf, err := json.Marshal(params)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(buf, &m); err != nil {
			return err
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
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}

	// Unmarshal to the destination type.
	return json.Unmarshal(buf, v)
}
