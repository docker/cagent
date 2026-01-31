package readfile

import (
	"fmt"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

func New(msg *types.Message, sessionState service.SessionStateReader) layout.Model {
	return toolcommon.NewBase(msg, sessionState, toolcommon.SimpleRendererWithResult(
		toolcommon.ExtractField(func(a builtin.ReadFileArgs) string { return toolcommon.ShortenPath(a.Path) }),
		extractResult,
	))
}

func extractResult(msg *types.Message) string {
	if msg.ToolResult == nil || msg.ToolResult.Meta == nil {
		return ""
	}
	meta, ok := msg.ToolResult.Meta.(builtin.ReadFileMeta)
	if !ok {
		return ""
	}
	if meta.Error != "" {
		return meta.Error
	}
	return fmt.Sprintf("%d lines", meta.LineCount)
}
