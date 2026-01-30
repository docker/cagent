package tools

import (
	"context"
	"encoding/json"
)

const (
	// DescriptionParam is the parameter name for the description
	DescriptionParam = "description"
)

// DescriptionToolSet wraps a ToolSet and adds a "description" parameter to all tools.
// This allows the LLM to provide context about what it's doing with each tool call.
type DescriptionToolSet struct {
	ToolSetWrapper
}

// NewDescriptionToolSet creates a new DescriptionToolSet wrapping the given ToolSet.
func NewDescriptionToolSet(inner ToolSet) *DescriptionToolSet {
	return &DescriptionToolSet{ToolSetWrapper: ToolSetWrapper{ToolSet: inner}}
}

func (f *DescriptionToolSet) Tools(ctx context.Context) ([]Tool, error) {
	tools, err := f.ToolSet.Tools(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Tool, len(tools))
	for i, tool := range tools {
		result[i] = f.addDescriptionParam(tool)
	}
	return result, nil
}

func (f *DescriptionToolSet) addDescriptionParam(tool Tool) Tool {
	if !tool.AddDescriptionParameter {
		return tool
	}

	schema, err := SchemaToMap(tool.Parameters)
	if err != nil {
		return tool
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		properties = make(map[string]any)
		schema["properties"] = properties
	}

	properties[DescriptionParam] = map[string]any{
		"type":        "string",
		"description": "A brief, human-readable description of what this tool call is doing",
	}

	tool.Parameters = schema
	return tool
}

// ExtractDescription extracts the description from tool call arguments.
func ExtractDescription(arguments string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}

	if desc, ok := args[DescriptionParam].(string); ok {
		return desc
	}
	return ""
}
