package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dop251/goja"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/docker/cagent/pkg/tools"
)

func (c *tool) runJavascript(ctx context.Context, script string) (string, error) {
	vm := goja.New()

	// Inject console object to the help the LLM debug its own code.
	_ = vm.Set("console", console())

	// Inject every tool as a javascript function.
	for _, toolset := range c.toolsets {
		allTools, err := toolset.Tools(ctx)
		if err != nil {
			return "", err
		}

		for _, tool := range allTools {
			_ = vm.Set(tool.Function.Name, callTool(ctx, tool))
		}
	}

	// Wrap the user script in an IIFE to allow top-level returns.
	script = "(() => {\n" + script + "\n})()"

	// Run the script.
	v, err := vm.RunString(script)
	if err != nil {
		return fmt.Sprintf("Error running script: %s", err), nil
	}

	// Some script are fire and forget and don't return anything.
	// In that case we return "done." to please the LLM which can't deal with empty responses.
	result := v.Export()
	if result == nil {
		return "<no output>", nil
	}

	return fmt.Sprintf("%v", result), nil
}

func callTool(ctx context.Context, tool tools.Tool) func(args map[string]any) (any, error) {
	return func(args map[string]any) (any, error) {
		nonNilArgs := make(map[string]any)
		for k, v := range args {
			if slices.Contains(tool.Function.Parameters.Required, k) || v != nil {
				nonNilArgs[k] = v
			}
		}

		arguments, err := json.Marshal(nonNilArgs)
		if err != nil {
			return nil, err
		}

		result, err := tool.Handler(ctx, tools.ToolCall{
			Function: tools.FunctionCall{
				Name:      tool.Function.Name,
				Arguments: string(arguments),
			},
		})
		if err != nil {
			return nil, err
		}

		// If the tool has a string output schema or no schema, return as string
		if tool.Function.OutputSchema == nil {
			return result.Output, nil
		}

		// Check if output schema indicates a string type
		if s, ok := tool.Function.OutputSchema.(*jsonschema.Schema); ok {
			if s.Type == "string" {
				return result.Output, nil
			}
		} else if schemaMap, ok := tool.Function.OutputSchema.(map[string]any); ok {
			if schemaType, hasType := schemaMap["type"].(string); hasType && schemaType == "string" {
				return result.Output, nil
			}
		}

		// For non-string schemas, try to parse JSON
		var parsed any
		if err := json.Unmarshal([]byte(result.Output), &parsed); err != nil {
			// If JSON parsing fails, return as string (fallback)
			return result.Output, nil
		}

		return parsed, nil
	}
}
