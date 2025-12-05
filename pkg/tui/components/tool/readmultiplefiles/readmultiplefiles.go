package readmultiplefiles

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

// Component is a specialized component for rendering read_multiple_files tool calls.
type Component struct {
	message *types.Message
	spinner spinner.Spinner
	width   int
	height  int
}

// New creates a new read multiple files component.
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
	var args builtin.ReadMultipleFilesArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &args); err != nil {
		return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), "Read Multiple Files", c.spinner.View(), "", c.width)
	}

	// For pending/running state, show files being read
	if msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning {
		params := formatFilesList(args.Paths)
		return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), "Read Multiple Files", params, c.spinner.View(), c.width)
	}

	// For completed/error state, render header line followed by each file line
	summaries := formatSummaryLines(args.Paths, msg.Content)

	// Build output with header and separate lines for each file
	var content strings.Builder

	// Header line
	icon := toolcommon.Icon(msg.ToolStatus)
	content.WriteString(fmt.Sprintf("%s %s:\n", icon, "Read Multiple Files"))

	// Each file on its own line with checkmark
	for _, summary := range summaries {
		content.WriteString(fmt.Sprintf("%s %s: %s\n", icon, summary.displayName, summary.params))
	}

	// Apply tool message styling
	return styles.RenderComposite(styles.ToolMessageStyle.Width(c.width-1), content.String())
}

type fileSummary struct {
	displayName string
	params      string
}

// formatSummaryLines creates a summary for each file
func formatSummaryLines(paths []string, result string) []fileSummary {
	if len(paths) == 0 {
		return nil
	}

	homeDir := getHomeDir()

	// Parse the result to count lines
	type PathContent struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	// Try to parse as JSON first
	var contents []PathContent
	if err := json.Unmarshal([]byte(result), &contents); err == nil {
		// JSON format
		summaries := make([]fileSummary, 0, len(contents))
		for _, content := range contents {
			shortPath := shortenPath(content.Path, homeDir)
			displayName := fmt.Sprintf("Read(%s)", shortPath)
			var params string
			if strings.HasPrefix(content.Content, "Error") {
				params = content.Content
			} else {
				lineCount := countLines(content.Content)
				params = fmt.Sprintf("Read %d lines", lineCount)
			}
			summaries = append(summaries, fileSummary{displayName: displayName, params: params})
		}
		return summaries
	}

	// Fall back to text format parsing
	// Format is: === path ===\ncontent\n\n
	summaries := make([]fileSummary, 0, len(paths))

	// Split by "=== " to get sections
	parts := strings.Split(result, "=== ")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Find the closing ===
		endIdx := strings.Index(part, " ===")
		if endIdx == -1 {
			continue
		}

		path := part[:endIdx]
		// Get content after " ===\n"
		contentStart := endIdx + 4 // len(" ===")
		if contentStart >= len(part) {
			continue
		}

		// Skip the newline after ===
		if contentStart < len(part) && part[contentStart] == '\n' {
			contentStart++
		}

		content := ""
		if contentStart < len(part) {
			content = part[contentStart:]
		}

		shortPath := shortenPath(path, homeDir)
		displayName := fmt.Sprintf("Read(%s)", shortPath)
		var params string
		if strings.HasPrefix(content, "Error") {
			params = strings.TrimSpace(content)
		} else {
			lineCount := countLines(content)
			params = fmt.Sprintf("Read %d lines", lineCount)
		}
		summaries = append(summaries, fileSummary{displayName: displayName, params: params})
	}

	if len(summaries) == 0 {
		// Fallback: just list the files
		for _, path := range paths {
			shortPath := shortenPath(path, homeDir)
			summaries = append(summaries, fileSummary{
				displayName: fmt.Sprintf("Read(%s)", shortPath),
				params:      "",
			})
		}
	}

	return summaries
}

// formatFilesList formats a list of file paths for display
func formatFilesList(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	// Shorten paths by using home directory symbol
	homeDir := getHomeDir()
	shortened := make([]string, len(paths))
	for i, p := range paths {
		shortened[i] = shortenPath(p, homeDir)
	}

	if len(shortened) == 1 {
		return shortened[0]
	}

	// For multiple files, show comma-separated list
	return strings.Join(shortened, ", ")
}

// shortenPath replaces home directory with ~ and shortens paths
func shortenPath(path, homeDir string) string {
	if homeDir != "" && strings.HasPrefix(path, homeDir) {
		return "~" + strings.TrimPrefix(path, homeDir)
	}
	return path
}

// getHomeDir returns the user's home directory
func getHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return ""
}

// countLines counts the number of lines in a string
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
