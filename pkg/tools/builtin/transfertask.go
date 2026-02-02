package builtin

import (
	"context"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameTransferTask = "transfer_task"

type TransferTaskTool struct{}

var _ tools.ToolSet = (*TransferTaskTool)(nil)

type TransferTaskArgs struct {
	Agent          string `json:"agent" jsonschema:"The name of the agent to transfer the task to."`
	Task           string `json:"task" jsonschema:"A clear and concise description of the task the member should achieve."`
	ExpectedOutput string `json:"expected_output" jsonschema:"The expected output from the member (optional)."`
}

func NewTransferTaskTool() *TransferTaskTool {
	return &TransferTaskTool{}
}

func (t *TransferTaskTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:     ToolNameTransferTask,
			Category: "transfer",
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
