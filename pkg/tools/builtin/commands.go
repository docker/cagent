package builtin

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

// CommandsTool exposes agent commands as callable tools.
type CommandsTool struct {
	tools.BaseToolSet
	commands map[string]string
}

var _ tools.ToolSet = (*CommandsTool)(nil)

func NewCommandsTool(commands map[string]string) *CommandsTool {
	return &CommandsTool{commands: commands}
}

func (c *CommandsTool) Instructions() string {
	if len(c.commands) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Agent Commands\n\n")

	for _, name := range c.sortedNames() {
		fmt.Fprintf(&b, "- **%s**: %s\n", name, truncate(c.commands[name], 80))
	}

	return b.String()
}

func (c *CommandsTool) Tools(_ context.Context) ([]tools.Tool, error) {
	if len(c.commands) == 0 {
		return nil, nil
	}

	result := make([]tools.Tool, 0, len(c.commands))
	for _, name := range c.sortedNames() {
		prompt := c.commands[name]
		result = append(result, tools.Tool{
			Name:        "command_" + name,
			Category:    "commands",
			Description: fmt.Sprintf("Execute '%s' command: %s", name, truncate(prompt, 100)),
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
				return &tools.ToolCallResult{
					Output: fmt.Sprintf("Execute: %s", prompt),
				}, nil
			},
			Annotations: tools.ToolAnnotations{ReadOnlyHint: true},
		})
	}

	return result, nil
}

func (c *CommandsTool) sortedNames() []string {
	return slices.Sorted(maps.Keys(c.commands))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
