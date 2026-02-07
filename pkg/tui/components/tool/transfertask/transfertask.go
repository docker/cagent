package transfertask

import (
	"encoding/json"
	"strings"

	"charm.land/lipgloss/v2"

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

func render(msg *types.Message, _ spinner.Spinner, _ service.SessionStateReader, width, _ int) string {
	var params builtin.TransferTaskArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &params); err != nil {
		return ""
	}

	header := styles.AgentBadgeStyle.MarginLeft(2).Render(msg.Sender) +
		" calls " +
		styles.AgentBadgeStyle.Render(params.Agent)

	// Calculate the icon with its margin
	icon := styles.ToolCompletedIcon.Render("✓")
	iconWithSpace := icon + " "
	iconWidth := lipgloss.Width(iconWithSpace)

	// Calculate available width for task text (accounting for icon width)
	availableWidth := max(width-iconWidth, 10)

	// Wrap the task text to fit within the available width
	lines := toolcommon.WrapLines(params.Task, availableWidth)

	// Build the task content with proper indentation for wrapped lines
	var taskContent strings.Builder
	for i, line := range lines {
		if i == 0 {
			// First line: icon + text
			taskContent.WriteString(iconWithSpace)
			taskContent.WriteString(styles.ToolMessageStyle.Render(line))
		} else {
			// Subsequent lines: indent to align with first line's text
			taskContent.WriteString("\n")
			taskContent.WriteString(strings.Repeat(" ", iconWidth))
			taskContent.WriteString(styles.ToolMessageStyle.Render(line))
		}
	}

	return header + "\n\n" + taskContent.String()
}
