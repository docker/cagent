package tool

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/glamour/v2"

	"github.com/docker/cagent/pkg/codemode"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

func renderSearchFiles(toolCall tools.ToolCall) string {
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

func renderRunToolsWithJavascript(toolCall tools.ToolCall, renderer *glamour.TermRenderer) string {
	var args codemode.RunToolsWithJavascriptArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return ""
	}

	md, err := renderer.Render("```javascript\n" + args.Script + "\n```")
	if err != nil {
		return args.Script
	}

	return md
}

func renderEditFile(toolCall tools.ToolCall, width int) (string, string) {
	var args builtin.EditFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", ""
	}

	var output strings.Builder
	for i, edit := range args.Edits {
		if i > 0 {
			output.WriteString("\n\n")
		}

		if len(args.Edits) > 1 {
			output.WriteString("Edit #" + string(rune(i+1+'0')) + ":\n")
		}

		diff := computeDiff(edit.OldText, edit.NewText)
		output.WriteString(renderDiffWithSyntaxHighlight(diff, args.Path, width))
	}

	return output.String(), args.Path
}

func renderShell(toolCall tools.ToolCall, renderer *glamour.TermRenderer) string {
	var args builtin.RunShellArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return ""
	}

	md, err := renderer.Render("```sh\n" + args.Cmd + "\n```")
	if err != nil {
		md = args.Cmd
	}

	if args.Cwd != "." {
		md += "\n  In directory: " + args.Cwd + "\n"
	}

	return md
}
