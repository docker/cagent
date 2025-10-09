package builtin

import (
	"context"

	"github.com/docker/cagent/pkg/tools"
)

type TransferTaskTool struct {
	elicitationTool
}

// Make sure Transfer Tool implements the ToolSet Interface
var _ tools.ToolSet = (*TransferTaskTool)(nil)

func NewTransferTaskTool() *TransferTaskTool {
	return &TransferTaskTool{}
}

func (t *TransferTaskTool) Instructions() string {
	return ""
}

func (t *TransferTaskTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Function: &tools.FunctionDefinition{
				Name: "transfer_task",
				Description: `Use this function to transfer a task to the selected team member.
            You must provide a clear and concise description of the task the member should achieve AND the expected output.`,
				Annotations: tools.ToolAnnotations{
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Transfer Task",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"agent": map[string]any{
							"type":        "string",
							"description": "The name of the agent to transfer the task to.",
						},
						"task": map[string]any{
							"type":        "string",
							"description": "A clear and concise description of the task the member should achieve.",
						},
						"expected_output": map[string]any{
							"type":        "string",
							"description": "The expected output from the member (optional).",
						},
					},
					Required: []string{"agent", "task", "expected_output"},
				},
			},
		},
	}, nil
}

func (t *TransferTaskTool) Start(context.Context) error {
	return nil
}

func (t *TransferTaskTool) Stop() error {
	return nil
}
