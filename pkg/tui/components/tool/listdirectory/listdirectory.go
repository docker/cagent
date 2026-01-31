package listdirectory

import (
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

func New(msg *types.Message, sessionState service.SessionStateReader) layout.Model {
	return toolcommon.NewBase(msg, sessionState, toolcommon.SimpleRendererWithResult(
		toolcommon.ExtractField(func(a builtin.ListDirectoryArgs) string { return toolcommon.ShortenPath(a.Path) }),
		extractResult,
	))
}

func extractResult(msg *types.Message) string {
	if msg.ToolResult == nil || msg.ToolResult.Meta == nil {
		return "empty directory"
	}
	meta, ok := msg.ToolResult.Meta.(builtin.ListDirectoryMeta)
	if !ok {
		return "empty directory"
	}

	fileCount := len(meta.Files)
	dirCount := len(meta.Dirs)
	if fileCount+dirCount == 0 {
		return "empty directory"
	}

	var parts []string
	if fileCount > 0 {
		parts = append(parts, formatCount(fileCount, "file", "files"))
	}
	if dirCount > 0 {
		parts = append(parts, formatCount(dirCount, "directory", "directories"))
	}

	result := strings.Join(parts, " and ")
	if meta.Truncated {
		result += " (truncated)"
	}
	return result
}

// formatCount returns a formatted count with proper singular/plural form.
func formatCount(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}
