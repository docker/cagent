package tool

import (
	"encoding/json"
	"strings"

	"github.com/docker/cagent/pkg/codemode"
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

func render_run_tools_with_javascript(toolCall tools.ToolCall) string {
	var args codemode.RunToolsWithJavascriptArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return ""
	}

	return args.Script
}

func render_edit_file(toolCall tools.ToolCall) (string, string) {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", ""
	}

	var output strings.Builder

	if len(args.Edits) > 0 {
		for i, edit := range args.Edits {
			if i > 0 {
				output.WriteString("\n\n")
			}

			if len(args.Edits) > 1 {
				output.WriteString("Edit #" + string(rune(i+1+'0')) + ":\n")
			}

			diff := computeDiff(edit.OldText, edit.NewText)
			output.WriteString(renderDiffWithSyntaxHighlight(diff, args.Path))
		}
	}

	return output.String(), args.Path
}
