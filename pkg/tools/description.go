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
	inner ToolSet
}

// NewDescriptionToolSet creates a new DescriptionToolSet wrapping the given ToolSet.
func NewDescriptionToolSet(inner ToolSet) *DescriptionToolSet {
	return &DescriptionToolSet{inner: inner}
}

func (f *DescriptionToolSet) Tools(ctx context.Context) ([]Tool, error) {
	tools, err := f.inner.Tools(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Tool, len(tools))
	for i, tool := range tools {
		result[i] = f.addDescriptionParam(tool)
	}
	return result, nil
}

func (f *DescriptionToolSet) Instructions() string {
	return f.inner.Instructions()
}

func (f *DescriptionToolSet) Start(ctx context.Context) error {
	return f.inner.Start(ctx)
}

func (f *DescriptionToolSet) Stop(ctx context.Context) error {
	return f.inner.Stop(ctx)
}

func (f *DescriptionToolSet) SetElicitationHandler(handler ElicitationHandler) {
	f.inner.SetElicitationHandler(handler)
}

func (f *DescriptionToolSet) SetOAuthSuccessHandler(handler func()) {
	f.inner.SetOAuthSuccessHandler(handler)
}

func (f *DescriptionToolSet) SetManagedOAuth(managed bool) {
	f.inner.SetManagedOAuth(managed)
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
