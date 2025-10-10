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

type TransferTaskArgs struct {
	Agent          string `json:"agent" jsonschema:"The name of the agent to transfer the task to."`
	Task           string `json:"task" jsonschema:"A clear and concise description of the task the member should achieve."`
	ExpectedOutput string `json:"expected_output" jsonschema:"The expected output from the member (optional)."`
}

func NewTransferTaskTool() *TransferTaskTool {
	return &TransferTaskTool{}
}

func (t *TransferTaskTool) Instructions() string {
	return ""
}

func (t *TransferTaskTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name: "transfer_task",
			Description: `Use this function to transfer a task to the selected team member.
            You must provide a clear and concise description of the task the member should achieve AND the expected output.`,
			Parameters: tools.MustSchemaFor[TransferTaskArgs](),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Transfer Task",
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
