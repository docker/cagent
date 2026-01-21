package editfile

import (
	"encoding/json"
	"fmt"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

type ToggleDiffViewMsg struct{}

func New(msg *types.Message, sessionState *service.SessionState) layout.Model {
	return toolcommon.NewBase(msg, sessionState, render)
}

func render(msg *types.Message, s spinner.Spinner, sessionState *service.SessionState, width, _ int) string {
	var args builtin.EditFileArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &args); err != nil {
		return ""
	}

	// Check for friendly description first
	var content string
	if header, ok := toolcommon.RenderFriendlyHeader(msg, s); ok {
		content = header
	} else {
		content = fmt.Sprintf("%s%s %s",
			toolcommon.Icon(msg, s),
			styles.ToolName.Render(msg.ToolDefinition.DisplayName()),
			styles.ToolMessageStyle.Render(toolcommon.ShortenPath(args.Path)))
	}

	if !sessionState.HideToolResults() {
		if msg.ToolCall.Function.Arguments != "" {
			contentWidth := width - styles.ToolCallResult.GetHorizontalFrameSize()
			content += "\n" + styles.ToolCallResult.Render(
				renderEditFile(msg.ToolCall, contentWidth, sessionState.SplitDiffView(), msg.ToolStatus))
		}

		if (msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
			content += toolcommon.FormatToolResult(msg.Content, width)
		}
	}

	return content
}
