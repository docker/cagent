package listdirectory

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Component is a specialized component for rendering list_directory tool calls.
type Component struct {
	message *types.Message
	spinner spinner.Spinner
	width   int
	height  int
}

// New creates a new list directory component.
func New(
	msg *types.Message,
	_ *service.SessionState,
) layout.Model {
	return &Component{
		message: msg,
		spinner: spinner.New(spinner.ModeSpinnerOnly),
		width:   80,
		height:  1,
	}
}

func (c *Component) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	return nil
}

func (c *Component) Init() tea.Cmd {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		return c.spinner.Init()
	}
	return nil
}

func (c *Component) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		var cmd tea.Cmd
		var model layout.Model
		model, cmd = c.spinner.Update(msg)
		c.spinner = model.(spinner.Spinner)
		return c, cmd
	}

	return c, nil
}

func (c *Component) View() string {
	msg := c.message

	// Parse arguments
	var args builtin.ListDirectoryArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &args); err != nil {
		return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), msg.ToolDefinition.DisplayName(), c.spinner.View(), "", c.width)
	}

	// Shorten the path for display
	shortPath := shortenPath(args.Path)
	displayName := fmt.Sprintf("%s(%s)", msg.ToolDefinition.DisplayName(), shortPath)

	// For pending/running state, show spinner
	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), displayName, c.spinner.View(), "", c.width)
	}

	// For completed/error state, show concise summary
	summary := formatSummary(msg.Content)
	params := styles.MutedStyle.Render(summary)

	return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), displayName, params, "", c.width)
}

// formatSummary creates a concise summary of the directory listing
func formatSummary(content string) string {
	if content == "" {
		return "empty directory"
	}

	// Count DIR and FILE entries
	lines := strings.Split(strings.TrimSpace(content), "\n")
	fileCount := 0
	dirCount := 0
	truncated := false

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "DIR  "):
			dirCount++
		case strings.HasPrefix(line, "FILE "):
			fileCount++
		case strings.Contains(line, "truncated"):
			truncated = true
		}
	}

	totalCount := fileCount + dirCount
	if totalCount == 0 {
		return "empty directory"
	}

	// Build the summary message
	var parts []string
	if fileCount > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s", fileCount, pluralize(fileCount)))
	}
	if dirCount > 0 {
		parts = append(parts, fmt.Sprintf("%d director%s", dirCount, pluralizeDirectory(dirCount)))
	}

	result := fmt.Sprintf("found %s", strings.Join(parts, " and "))
	if truncated {
		result += " (truncated)"
	}

	return result
}

func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func pluralizeDirectory(count int) string {
	if count == 1 {
		return "y"
	}
	return "ies"
}

// shortenPath replaces home directory with ~ for cleaner display
func shortenPath(path string) string {
	if path == "" {
		return path
	}

	// Replace home directory with ~
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}

	// If path is ".", return current directory name or "."
	if path == "." {
		return "."
	}

	return path
}
