package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

const prompt = `Run a Javascript script to call MCP tools.

Instead of calling individual tools directly, use this to write a Javascript script that calls as many tools as needed.
This allows you to combine multiple tool calls in a single request, perform conditional logic,
and manipulate the results before returning them.

Instructions:
 - The script has access to all the tools as plain javascript functions.
 - "await"/"async" are never needed. All the tool calls are synchronous.
 - Every tool function returns a string result.
 - The script must return a string result.

Available tools/functions:

`

func Wrap(toolsets []tools.ToolSet) tools.ToolSet {
	return &codeModeTool{
		toolsets: toolsets,
	}
}

type codeModeTool struct {
	toolsets []tools.ToolSet
}

type RunToolsWithJavascriptArgs struct {
	Script string `json:"script" jsonschema:"Script to execute"`
}

func (c *codeModeTool) Instructions() string {
	return ""
}

func (c *codeModeTool) Tools(ctx context.Context) ([]tools.Tool, error) {
	var functionsDoc []string

	for _, toolset := range c.toolsets {
		allTools, err := toolset.Tools(ctx)
		if err != nil {
			return nil, err
		}

		for _, tool := range allTools {
			functionsDoc = append(functionsDoc, toolToJsDoc(tool))
		}
	}

	return []tools.Tool{{
		Name:        "run_tools_with_javascript",
		Description: prompt + strings.Join(functionsDoc, "\n"),
		Parameters:  tools.MustSchemaFor[RunToolsWithJavascriptArgs](),
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			var args RunToolsWithJavascriptArgs
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parsing arguments: %w", err)
			}

			output, err := c.runJavascript(ctx, args.Script)
			if err != nil {
				return nil, err
			}

			return &tools.ToolCallResult{
				Output: output,
			}, nil
		},
		Annotations: tools.ToolAnnotations{
			Title: "Run tools with Javascript",
		},
	}}, nil
}

func (c *codeModeTool) Start(ctx context.Context) error {
	for _, t := range c.toolsets {
		if err := t.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *codeModeTool) Stop() error {
	var errs []error

	for _, t := range c.toolsets {
		if err := t.Stop(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *codeModeTool) SetElicitationHandler(tools.ElicitationHandler) {
	// No-op, this tool does not use elicitation
}

func (c *codeModeTool) SetOAuthSuccessHandler(func()) {
	// No-op, this tool does not use OAuth
}
