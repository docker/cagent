package transfertask

import (
	"github.com/docker/cagent/pkg/tui/components/tool/subagent"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

// New creates a new transfer task view using the subagent tree component
func New(msg *types.Message, sessionState *service.SessionState) layout.Model {
	return subagent.New(msg, sessionState)
}
