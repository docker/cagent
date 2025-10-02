package tools

import "reflect"

func ToOutputSchemaSchema(valueType reflect.Type) ToolOutputSchema {
	seen := map[reflect.Type]bool{}

	schemaMap := toOutputSchemaSchema(valueType, seen)

	schema := ToolOutputSchema{}
	if vType := schemaMap["type"]; vType != nil {
		schema.Type = vType.(string)
	}
	if vRef := schemaMap["$ref"]; vRef != nil {
		schema.Ref = vRef.(string)
	}
	if vProperties := schemaMap["properties"]; vProperties != nil {
		schema.Properties = vProperties.(map[string]any)
	}
	if vItems := schemaMap["items"]; vItems != nil {
		schema.Items = vItems.(map[string]any)
	}

	return schema
}

func toOutputSchemaSchema(valueType reflect.Type, seen map[reflect.Type]bool) map[string]any {
	// TODO(dga): support more complicated references.
	if seen[valueType] {
		return map[string]any{
			"$ref": "#",
		}
	}

	elemType := valueType.Kind()
	switch elemType {
	case reflect.String:
		return map[string]any{
			"type": "string",
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{
			"type": "int",
		}
	case reflect.Float64, reflect.Float32:
		return map[string]any{
			"type": "number",
		}
	case reflect.Bool:
		return map[string]any{
			"type": "boolean",
		}
	case reflect.Slice:
		return map[string]any{
			"type":  "array",
			"items": toOutputSchemaSchema(valueType.Elem(), seen),
		}
	case reflect.Pointer:
		return toOutputSchemaSchema(valueType.Elem(), seen)
	default:
		seen[valueType] = true

		properties := map[string]any{}
		for i := range valueType.NumField() {
			field := valueType.Field(i)

			name := field.Name
			if jsonTag, ok := field.Tag.Lookup("json"); ok {
				name = jsonTag
			}

			fieldSchema := toOutputSchemaSchema(field.Type, seen)
			if fieldDesc, ok := field.Tag.Lookup("description"); ok {
				fieldSchema["description"] = fieldDesc
			}

			properties[name] = fieldSchema
		}

		return map[string]any{
			"type":       "object",
			"properties": properties,
		}
	}
}
