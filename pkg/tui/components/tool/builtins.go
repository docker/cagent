package tool

import (
	"encoding/json"
	"strings"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

func render_search_files(toolCall tools.ToolCall) string {
	var args builtin.SearchFilesArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return ""
	}

	// Search pattern
	output := args.Pattern

	// Optional search path
	if path := args.Path; path != "" && path != "." {
		output += " in " + path
	}

	// Optional exclude patterns
	if exclude := args.ExcludePatterns; len(exclude) > 0 {
		output += " excluding [" + strings.Join(exclude, ", ") + "]"
	}

	return output
}
