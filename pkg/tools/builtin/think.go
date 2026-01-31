package builtin

import (
	"context"
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameThink = "think"

type ThinkTool struct {
	thoughts []string
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*ThinkTool)(nil)
	_ tools.Instructable = (*ThinkTool)(nil)
)

type ThinkArgs struct {
	Thought string `json:"thought" jsonschema:"The thought to think about"`
}

func (t *ThinkTool) callTool(_ context.Context, params ThinkArgs) (*tools.ToolCallResult, error) {
	t.thoughts = append(t.thoughts, params.Thought)
	return tools.ResultSuccess("Thoughts:\n" + strings.Join(t.thoughts, "\n")), nil
}

func NewThinkTool() *ThinkTool {
	return &ThinkTool{}
}

func (t *ThinkTool) Instructions() string {
	return `## Using the think tool

Before taking any action or responding to the user after receiving tool results, use the think tool as a scratchpad to:
- List the specific rules that apply to the current request
- Check if all required information is collected
- Verify that the planned action complies with all policies
- Iterate over tool results for correctness

## Rules
- Use the think tool generously to jot down thoughts and ideas.`
}

func (t *ThinkTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         ToolNameThink,
			Category:     "think",
			Description:  "Use the tool to think about something. It will not obtain new information or change the database, but just append the thought to the log. Use it when complex reasoning or some cache memory is needed.",
			Parameters:   tools.MustSchemaFor[ThinkArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.callTool),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Think",
			},
		},
	}, nil
}
