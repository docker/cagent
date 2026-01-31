package searchfilescontent

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
		extractArgs,
		extractResult,
	))
}

func extractArgs(args string) string {
	parsed, err := toolcommon.ParseArgs[builtin.SearchFilesContentArgs](args)
	if err != nil {
		return ""
	}

	path := toolcommon.ShortenPath(parsed.Path)
	query := parsed.Query
	if len(query) > 30 {
		query = query[:27] + "..."
	}

	if parsed.IsRegex {
		return fmt.Sprintf("%s (regex: %s)", path, query)
	}
	return fmt.Sprintf("%s (%s)", path, query)
}

func extractResult(msg *types.Message) string {
	if msg.ToolResult == nil || msg.ToolResult.Meta == nil {
		return "no matches"
	}
	meta, ok := msg.ToolResult.Meta.(builtin.SearchFilesContentMeta)
	if !ok {
		return "no matches"
	}

	if meta.MatchCount == 0 {
		return "no matches"
	}

	matchWord := "match"
	if meta.MatchCount != 1 {
		matchWord = "matches"
	}

	fileWord := "file"
	if meta.FileCount != 1 {
		fileWord = "files"
	}

	return fmt.Sprintf("%d %s in %d %s", meta.MatchCount, matchWord, meta.FileCount, fileWord)
}
