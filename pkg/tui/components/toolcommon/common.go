package toolcommon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

func Icon(status types.ToolStatus) string {
	switch status {
	case types.ToolStatusPending:
		return "⊙"
	case types.ToolStatusRunning:
		return "⚙"
	case types.ToolStatusCompleted:
		return styles.SuccessStyle.Render("✓")
	case types.ToolStatusError:
		return styles.ErrorStyle.Render("✗")
	case types.ToolStatusConfirmation:
		return styles.WarningStyle.Render("?")
	default:
		return styles.WarningStyle.Render("?")
	}
}

func FormatToolResult(content string, width int) string {
	var formattedContent string
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		formattedContent = content
	} else if buf, err := json.MarshalIndent(m, "", "  "); err != nil {
		formattedContent = content
	} else {
		formattedContent = string(buf)
	}

	padding := styles.ToolCallResult.Padding().GetHorizontalPadding()
	availableWidth := max(width-2-padding, 10) // Minimum readable width

	lines := wrapLines(formattedContent, availableWidth)

	header := "output"
	if len(lines) > 10 {
		lines = lines[:10]
		header = "output (truncated)"
		lines = append(lines, wrapLines("...", availableWidth)...)
	}

	trimmedContent := strings.Join(lines, "\n")
	if trimmedContent != "" {
		return "\n" + styles.ToolCallResult.Render(styles.ToolCallResultKey.Render("\n-> "+header+":")+"\n"+trimmedContent)
	}

	return ""
}

func RenderTool(icon, name, params, result string, width int) string {
	content := fmt.Sprintf("%s %s %s", icon, styles.HighlightStyle.Render(name), styles.MutedStyle.Render(params))
	if result != "" {
		content += "\n" + result
	}
	return styles.RenderComposite(styles.ToolMessageStyle.Width(width-1), content)
}

func wrapLines(text string, width int) []string {
	if width <= 0 {
		return strings.Split(text, "\n")
	}

	var lines []string

	for line := range strings.SplitSeq(text, "\n") {
		for len(line) > width {
			lines = append(lines, line[:width])
			line = line[width:]
		}

		lines = append(lines, line)
	}

	return lines
}
