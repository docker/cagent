package builtin

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/tools"
)

type ScriptShellTool struct {
	shellTools map[string]latest.ScriptShellToolConfig
	env        []string
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*ScriptShellTool)(nil)
	_ tools.Instructable = (*ScriptShellTool)(nil)
)

func NewScriptShellTool(shellTools map[string]latest.ScriptShellToolConfig, env []string) (*ScriptShellTool, error) {
	for toolName, tool := range shellTools {
		if err := validateConfig(toolName, tool); err != nil {
			return nil, err
		}
	}

	return &ScriptShellTool{
		shellTools: shellTools,
		env:        env,
	}, nil
}

func validateConfig(toolName string, tool latest.ScriptShellToolConfig) error {
	// If no required array was set, all arguments are required
	if tool.Required == nil {
		tool.Required = make([]string, 0, len(tool.Args))
		for argName := range tool.Args {
			tool.Required = append(tool.Required, argName)
		}
	}

	// Check for typos in args
	var missingArgs []string
	os.Expand(tool.Cmd, func(varName string) string {
		if _, ok := tool.Args[varName]; !ok {
			missingArgs = append(missingArgs, varName)
		}
		return ""
	})
	if len(missingArgs) > 0 {
		return fmt.Errorf("tool '%s' uses undefined args: %v", toolName, missingArgs)
	}

	// Check that all required args are defined
	for _, reqArg := range tool.Required {
		if _, ok := tool.Args[reqArg]; !ok {
			return fmt.Errorf("tool '%s' has required arg '%s' which is not defined in args", toolName, reqArg)
		}
	}

	return nil
}

func (t *ScriptShellTool) Instructions() string {
	var instructions strings.Builder
	instructions.WriteString("## Custom Shell Tools\n\n")
	instructions.WriteString("The following custom shell tools are available:\n\n")

	for name, tool := range t.shellTools {
		fmt.Fprintf(&instructions, "### %s\n", name)
		if tool.Description != "" {
			fmt.Fprintf(&instructions, "%s\n\n", tool.Description)
		} else {
			fmt.Fprintf(&instructions, "Execute: `%s`\n\n", tool.Cmd)
		}

		if len(tool.Args) > 0 {
			instructions.WriteString("**Parameters:**\n")
			for argName, argDef := range tool.Args {
				required := ""
				if slices.Contains(tool.Required, argName) {
					required = " (required)"
				}
				description := argDef.(map[string]any)["description"].(string)
				fmt.Fprintf(&instructions, "- `%s`: %s%s\n", argName, description, required)
			}
			instructions.WriteString("\n")
		}
	}

	return instructions.String()
}

func (t *ScriptShellTool) Tools(context.Context) ([]tools.Tool, error) {
	var toolsList []tools.Tool

	for name, toolConfig := range t.shellTools {
		cfg := toolConfig
		toolName := name

		description := cmp.Or(cfg.Description, fmt.Sprintf("Execute shell command: %s", cfg.Cmd))

		inputSchema, err := tools.SchemaToMap(map[string]any{
			"type":       "object",
			"properties": cfg.Args,
			"required":   cfg.Required,
		})
		if err != nil {
			return nil, fmt.Errorf("invalid schema for tool %s: %w", toolName, err)
		}

		toolsList = append(toolsList, tools.Tool{
			Name:         toolName,
			Description:  description,
			Parameters:   inputSchema,
			OutputSchema: tools.MustSchemaFor[string](),
			Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
				return t.execute(ctx, &cfg, toolCall)
			},
		})
	}

	return toolsList, nil
}

func (t *ScriptShellTool) execute(ctx context.Context, toolConfig *latest.ScriptShellToolConfig, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params map[string]any
	if toolCall.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	// Use default shell
	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")

	cmd := exec.CommandContext(ctx, shell, "-c", toolConfig.Cmd)
	cmd.Dir = toolConfig.WorkingDir
	cmd.Env = t.env
	for key, value := range params {
		if value != nil {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error executing command '%s': %s\nOutput: %s", toolConfig.Cmd, err, limitOutput(string(output)))), nil
	}

	return tools.ResultSuccess(limitOutput(string(output))), nil
}
