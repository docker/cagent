package codemode

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/docker/cagent/pkg/tools"
)

// schemaToJSType converts a JSON schema to a JavaScript/TypeScript type string
func schemaToJSType(schema any) string {
	if schema == nil {
		return "any"
	}

	// Handle jsonschema.Schema type from the Google jsonschema library
	if s, ok := schema.(*jsonschema.Schema); ok {
		return schemaToJSTypeFromStruct(s)
	}

	// Handle boolean schema (any type)
	if boolSchema, ok := schema.(bool); ok {
		if boolSchema {
			return "any"
		}
		return "never"
	}

	// Handle object schema (map)
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return "any"
	}

	schemaType, hasType := schemaMap["type"].(string)
	if !hasType {
		return "any"
	}

	switch schemaType {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if items, hasItems := schemaMap["items"]; hasItems {
			itemType := schemaToJSType(items)
			return itemType + "[]"
		}
		return "any[]"
	case "object":
		// For complex objects, return 'object' for simplicity in JS context
		return "object"
	default:
		return "any"
	}
}

func schemaToJSTypeFromStruct(s *jsonschema.Schema) string {
	switch s.Type {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if s.Items != nil {
			itemType := schemaToJSType(s.Items)
			return itemType + "[]"
		}
		return "any[]"
	case "object":
		// For complex objects, return 'object' for simplicity in JS context
		return "object"
	default:
		return "any"
	}
}

func toolToJsDoc(tool tools.Tool) string {
	var doc strings.Builder

	// Determine return type from output schema
	returnType := "any" // default fallback when no schema is available
	if tool.Function.OutputSchema != nil {
		returnType = schemaToJSType(tool.Function.OutputSchema)
	}

	doc.WriteString("===== " + tool.Function.Name + " =====\n\n")
	doc.WriteString(strings.TrimSpace(tool.Function.Description))
	doc.WriteString("\n\n")
	if len(tool.Function.Parameters.Properties) == 0 {
		doc.WriteString(fmt.Sprintf("%s(): %s\n", tool.Function.Name, returnType))
	} else {
		doc.WriteString(fmt.Sprintf("%s(args: ArgsObject): %s\n", tool.Function.Name, returnType))
		doc.WriteString("\nwhere type ArgsObject = {\n")
		for paramName, param := range tool.Function.Parameters.Properties {
			pType := "Object"

			var (
				pDesc string
				pEnum string
			)
			if paramMap, ok := param.(map[string]any); ok {
				if t, ok := paramMap["type"].(string); ok {
					pType = t
				}
				if d, ok := paramMap["description"].(string); ok {
					pDesc = d
				}
				if values, ok := paramMap["enum"].([]any); ok {
					for _, v := range values {
						if pEnum != "" {
							pEnum += " | "
						}
						if pType == "string" {
							pEnum += fmt.Sprintf("'%v'", v)
						} else {
							pEnum += fmt.Sprintf("%v", v)
						}
					}
				}
			}

			if !slices.Contains(tool.Function.Parameters.Required, paramName) {
				paramName += "?"
			}

			if pEnum != "" {
				doc.WriteString(fmt.Sprintf("  %s: %s // %s\n", paramName, pEnum, strings.TrimSpace(pDesc)))
			} else {
				doc.WriteString(fmt.Sprintf("  %s: %s // %s\n", paramName, pType, strings.TrimSpace(pDesc)))
			}
		}
		doc.WriteString("};\n")
	}

	return doc.String()
}
