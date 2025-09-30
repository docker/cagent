package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/tools"
)

type ScriptShellTool struct {
	shellTools map[string]latest.ScriptShellToolConfig
	env        []string
}

var _ tools.ToolSet = (*ScriptShellTool)(nil)

func NewScriptShellTool(shellTools map[string]latest.ScriptShellToolConfig, env []string) *ScriptShellTool {
	for _, tool := range shellTools {
		// If no required array was set, all arguments are required
		if tool.Required == nil {
			tool.Required = make([]string, len(tool.Args))
			for argName := range tool.Args {
				tool.Required = append(tool.Required, argName)
			}
		}
	}
	return &ScriptShellTool{
		shellTools: shellTools,
		env:        env,
	}
}

func (t *ScriptShellTool) Instructions() string {
	var instructions strings.Builder
	instructions.WriteString("## Custom Shell Tools\n\n")
	instructions.WriteString("The following custom shell tools are available:\n\n")

	for name, tool := range t.shellTools {
		instructions.WriteString(fmt.Sprintf("### %s\n", name))
		if tool.Description != "" {
			instructions.WriteString(fmt.Sprintf("%s\n\n", tool.Description))
		} else {
			instructions.WriteString(fmt.Sprintf("Execute: `%s`\n\n", tool.Cmd))
		}

		if len(tool.Args) > 0 {
			instructions.WriteString("**Parameters:**\n")
			for argName, argDef := range tool.Args {
				required := ""
				if slices.Contains(tool.Required, argName) {
					required = " (required)"
				}
				description := argDef.(map[string]any)["description"].(string)
				instructions.WriteString(fmt.Sprintf("- `%s`: %s%s\n", argName, description, required))
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

		description := cfg.Description
		if description == "" {
			description = fmt.Sprintf("Execute shell command: %s", cfg.Cmd)
		}

		toolsList = append(toolsList, tools.Tool{
			Function: &tools.FunctionDefinition{
				Name:        toolName,
				Description: description,
				Parameters: tools.FunctionParameters{
					Type:       "object",
					Properties: cfg.Args,
					Required:   cfg.Required,
				},
			},
			Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
				return t.execute(ctx, &cfg, toolCall)
			},
		})
	}

	return toolsList, nil
}

func (t *ScriptShellTool) execute(ctx context.Context, toolConfig *latest.ScriptShellToolConfig, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Use default shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

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
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Error executing command '%s': %s\nOutput: %s", toolConfig.Cmd, err, string(output)),
		}, nil
	}

	return &tools.ToolCallResult{
		Output: string(output),
	}, nil
}

func (t *ScriptShellTool) Start(context.Context) error {
	return nil
}

func (t *ScriptShellTool) Stop() error {
	return nil
}
