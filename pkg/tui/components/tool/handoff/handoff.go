package handoff

import (
	"encoding/json"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

func New(msg *types.Message, sessionState service.SessionStateReader) layout.Model {
	return toolcommon.NewBase(msg, sessionState, render)
}

func render(msg *types.Message, _ spinner.Spinner, _ service.SessionStateReader, _, _ int) string {
	var params builtin.HandoffArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &params); err != nil {
		return ""
	}

	return styles.AgentBadgeStyle.MarginLeft(2).Render(msg.Sender) + " ─► " + styles.AgentBadgeStyle.Render(params.Agent)
}
