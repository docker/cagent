package taskstool

import (
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/styles"
)

func renderTaskIcon(status string) (string, lipgloss.Style) {
	switch status {
	case "pending":
		return "□", styles.ToBeDoneStyle
	case "in-progress":
		return "■", styles.InProgressStyle
	case "completed":
		return "✓", styles.CompletedStyle
	default:
		return "?", styles.ToBeDoneStyle
	}
}
