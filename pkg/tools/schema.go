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
	if m["required"] == nil {
		delete(m, "required")
	}

	// Ensure all properties have a type set, recursively.
	ensurePropertyTypes(m)

	return m, nil
}

// ensurePropertyTypes recursively walks a JSON Schema map and ensures
// every property has a "type" set, defaulting to "object" if missing.
// It descends into nested "properties" and array "items".
func ensurePropertyTypes(schema map[string]any) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}

	for _, v := range props {
		prop, ok := v.(map[string]any)
		if !ok {
			continue
		}

		if prop["type"] == nil {
			prop["type"] = "object"
		}

		// Recurse into nested object properties.
		ensurePropertyTypes(prop)

		// Recurse into array items.
		if items, ok := prop["items"].(map[string]any); ok {
			ensurePropertyTypes(items)
		}
	}
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
