package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/styles"
)

func renderEditFile(toolCall tools.ToolCall, width int, splitView bool) (string, string) {
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

		diff := computeDiff(args.Path, edit.OldText, edit.NewText)
		if splitView {
			output.WriteString(renderSplitDiffWithSyntaxHighlight(diff, args.Path, width))
		} else {
			output.WriteString(renderDiffWithSyntaxHighlight(diff, args.Path, width))
		}
	}

	return output.String(), args.Path
}

func renderToolArgs(toolCall tools.ToolCall, width int) string {
	decoder := json.NewDecoder(strings.NewReader(toolCall.Function.Arguments))

	tok, err := decoder.Token()
	if err != nil {
		return ""
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return ""
	}

	type kv struct {
		Key   string
		Value any
	}
	var kvs []kv

	for decoder.More() {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		key, ok := tok.(string)
		if !ok {
			return ""
		}

		var val any
		if err := decoder.Decode(&val); err != nil {
			return ""
		}

		kvs = append(kvs, kv{Key: key, Value: val})
	}
	_, _ = decoder.Token()

	style := styles.ToolCallArgs.Width(width)

	var md strings.Builder
	for i, kv := range kvs {
		if i > 0 {
			md.WriteString("\n")
		}

		var content string
		if v, ok := kv.Value.(string); ok {
			content = v
		} else {
			buf, err := json.MarshalIndent(kv.Value, "", "  ")
			if err != nil {
				content = fmt.Sprintf("%v", kv.Value)
			} else {
				content = string(buf)
			}
		}

		fmt.Fprintf(&md, "%s:\n%s", styles.ToolCallArgKey.Render(kv.Key), content)
		if !strings.HasSuffix(content, "\n") {
			md.WriteString("\n")
		}
	}

	return "\n" + style.Render(strings.TrimSuffix(md.String(), "\n"))
}
