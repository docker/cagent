package builtin

import (
	"context"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameHandoff = "handoff"

type HandoffTool struct{}

var _ tools.ToolSet = (*HandoffTool)(nil)

type HandoffArgs struct {
	Agent string `json:"agent" jsonschema:"The name of the agent to hand off the conversation to."`
}

func NewHandoffTool() *HandoffTool {
	return &HandoffTool{}
}

func (t *HandoffTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:        ToolNameHandoff,
			Category:    "handoff",
			Description: "Use this function to hand off the conversation to the selected agent.",
			Parameters:  tools.MustSchemaFor[HandoffArgs](),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Handoff Conversation",
			},
		},
	}, nil
}
