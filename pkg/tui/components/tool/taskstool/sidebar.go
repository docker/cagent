package taskstool

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/tab"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/styles"
)

// SidebarComponent represents the tasks display component for the sidebar
type SidebarComponent struct {
	tasks []builtin.Task
	width int
}

func NewSidebarComponent() *SidebarComponent {
	return &SidebarComponent{
		width: 20,
	}
}

func (c *SidebarComponent) SetSize(width int) {
	c.width = width
}

func (c *SidebarComponent) SetTasks(result *tools.ToolCallResult) error {
	if result == nil || result.Meta == nil {
		return nil
	}

	tasks, ok := result.Meta.([]builtin.Task)
	if !ok {
		return nil
	}

	c.tasks = tasks
	return nil
}

func (c *SidebarComponent) Render() string {
	if len(c.tasks) == 0 {
		return ""
	}

	var lines []string

	// Add summary stats
	lines = append(lines, c.renderStats())
	lines = append(lines, "")

	// Render each task
	for _, task := range c.tasks {
		lines = append(lines, c.renderTaskLine(task))
	}

	return c.renderTab("Tasks", strings.Join(lines, "\n"))
}

func (c *SidebarComponent) renderStats() string {
	var completed, inProgress, pending, blocked int
	for _, task := range c.tasks {
		switch task.Status {
		case "completed":
			completed++
		case "in-progress":
			inProgress++
		default:
			pending++
			if len(task.BlockedBy) > 0 && !c.allBlockersCompleted(task.BlockedBy) {
				blocked++
			}
		}
	}

	var parts []string
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d done", completed))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d active", inProgress))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if blocked > 0 {
		parts = append(parts, styles.WarningStyle.Render(fmt.Sprintf("%d blocked", blocked)))
	}

	return strings.Join(parts, " · ")
}

func (c *SidebarComponent) allBlockersCompleted(blockerIDs []string) bool {
	for _, blockerID := range blockerIDs {
		for _, task := range c.tasks {
			if task.ID == blockerID && task.Status != "completed" {
				return false
			}
		}
	}
	return true
}

func (c *SidebarComponent) renderTaskLine(task builtin.Task) string {
	icon, iconStyle := renderTaskIcon(task.Status)

	// Check if blocked
	isBlocked := len(task.BlockedBy) > 0 && !c.allBlockersCompleted(task.BlockedBy)
	if isBlocked && task.Status == "pending" {
		icon = "⚠"
		iconStyle = styles.WarningStyle
	}

	// Build the line
	prefix := iconStyle.Render(icon) + " "
	prefixWidth := lipgloss.Width(prefix)

	// Calculate available width for description
	maxDescWidth := c.width - prefixWidth

	// Add owner suffix if present
	var ownerSuffix string
	if task.Owner != "" {
		ownerSuffix = styles.MutedStyle.Render(" (" + task.Owner + ")")
		maxDescWidth -= lipgloss.Width(ownerSuffix)
	}

	description := toolcommon.TruncateText(task.Description, maxDescWidth)

	// Apply strikethrough for completed items
	if task.Status == "completed" {
		description = styles.CompletedStyle.Strikethrough(true).Render(description)
	} else {
		description = styles.TabPrimaryStyle.Render(description)
	}

	line := prefix + description + ownerSuffix

	// Add blocked-by indicator on next line if blocked
	if isBlocked {
		blockerText := styles.MutedStyle.Render("  → blocked by: " + strings.Join(task.BlockedBy, ", "))
		line += "\n" + toolcommon.TruncateText(blockerText, c.width)
	}

	return line
}

func (c *SidebarComponent) renderTab(title, content string) string {
	return tab.Render(title, content, c.width)
}
