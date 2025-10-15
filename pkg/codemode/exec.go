package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/dop251/goja"

	"github.com/docker/cagent/pkg/tools"
)

type ScriptResult struct {
	Value  string `json:"value" jsonschema:"The value returned by the script"`
	StdOut string `json:"stdout" jsonschema:"The standard output of the console"`
	StdErr string `json:"stderr" jsonschema:"The standard error of the console"`
}

func (c *codeModeTool) runJavascript(ctx context.Context, script string) (ScriptResult, error) {
	vm := goja.New()

	// Inject console object to the help the LLM debug its own code.
	var (
		stdOut bytes.Buffer
		stdErr bytes.Buffer
	)
	_ = vm.Set("console", console(&stdOut, &stdErr))

	// Inject every tool as a javascript function.
	for _, toolset := range c.toolsets {
		allTools, err := toolset.Tools(ctx)
		if err != nil {
			return ScriptResult{}, err
		}

		for _, tool := range allTools {
			_ = vm.Set(tool.Name, callTool(ctx, tool))
		}
	}

	// Wrap the user script in an IIFE to allow top-level returns.
	script = "(() => {\n" + script + "\n})()"

	// Run the script.
	v, err := vm.RunString(script)
	if err != nil {
		return ScriptResult{
			StdOut: stdOut.String(),
			StdErr: stdErr.String(),
			Value:  err.Error(),
		}, nil
	}

	value := ""
	if result := v.Export(); result != nil {
		value = fmt.Sprintf("%v", result)
	}

	return ScriptResult{
		StdOut: stdOut.String(),
		StdErr: stdErr.String(),
		Value:  value,
	}, nil
}

func callTool(ctx context.Context, tool tools.Tool) func(args map[string]any) (string, error) {
	return func(args map[string]any) (string, error) {
		nonNilArgs := make(map[string]any)
		for k, v := range args {
			// if slices.Contains(tool.Parameters.Required, k) || v != nil {
			nonNilArgs[k] = v
			// }
		}

		arguments, err := json.Marshal(nonNilArgs)
		if err != nil {
			return "", err
		}

		result, err := tool.Handler(ctx, tools.ToolCall{
			Function: tools.FunctionCall{
				Name:      tool.Name,
				Arguments: string(arguments),
			},
		})
		if err != nil {
			return "", err
		}

		return result.Output, nil
	}
}
