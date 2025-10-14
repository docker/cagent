package builtin

import "github.com/docker/cagent/pkg/tools"

type elicitationTool struct{}

func (t *elicitationTool) SetElicitationHandler(tools.ElicitationHandler) {
	// No-op, this tool does not use elicitation
}

func (t *elicitationTool) SetOAuthSuccessHandler(func()) {
	// No-op, this tool does not use OAuth
}
