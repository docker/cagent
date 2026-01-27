package toolcommon

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// ParseArgs unmarshals JSON arguments into a typed struct.
// Returns an error if parsing fails.
func ParseArgs[T any](args string) (T, error) {
	var result T
	var err error

	if err = json.Unmarshal([]byte(args), &result); err == nil {
		return result, nil
	}

	if fixed, ok := tryFixPartialJSON(args); ok {
		if partialErr := json.Unmarshal([]byte(fixed), &result); partialErr == nil {
			return result, nil
		}
	}

	return result, err
}

// tryFixPartialJSON attempts to complete a partial JSON object by closing
// any unclosed strings, arrays, and objects. Returns the fixed JSON and
// true if a fix was attempted, or the original string and false if input
// is empty or not a valid JSON object start.
func tryFixPartialJSON(s string) (string, bool) {
	if s == "" || s[0] != '{' {
		return s, false
	}

	var result strings.Builder
	result.WriteString(s)

	inString := false
	escaped := false
	var stack []byte

	for _, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch r {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if inString {
		result.WriteByte('"')
	}

	for i := len(stack) - 1; i >= 0; i-- {
		result.WriteByte(stack[i])
	}

	return result.String(), true
}

// ExtractField creates an argument extractor function that parses JSON and extracts a field.
// The field function receives the parsed args and returns the display string.
// It supports partial JSON parsing for streaming tool calls.
func ExtractField[T any](field func(T) string) func(string) string {
	return func(args string) string {
		parsed, err := ParseArgs[T](args)
		if err != nil {
			return ""
		}
		return field(parsed)
	}
}

func Icon(msg *types.Message, inProgress spinner.Spinner) string {
	switch msg.ToolStatus {
	case types.ToolStatusRunning, types.ToolStatusPending:
		// Animated spinner for both executing and streaming tool calls.
		// With centralized animation ticks, all spinners share a single tick
		// so there's no performance penalty for multiple animated spinners.
		return styles.NoStyle.MarginLeft(2).Render(inProgress.View())
	case types.ToolStatusCompleted:
		return styles.ToolCompletedIcon.Render("✓")
	case types.ToolStatusError:
		return styles.ToolErrorIcon.Render("✗")
	case types.ToolStatusConfirmation:
		return styles.ToolPendingIcon.Render("?")
	default:
		return styles.WarningStyle.Render("?")
	}
}

func FormatToolResult(content string, width int) string {
	var formattedContent string
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		formattedContent = content
	} else if buf, err := json.MarshalIndent(m, "", "  "); err != nil {
		formattedContent = content
	} else {
		formattedContent = string(buf)
	}

	availableWidth := max(width-styles.ToolCallResult.GetHorizontalFrameSize(), 10) // Minimum readable width

	lines := WrapLines(formattedContent, availableWidth)

	if len(lines) > 10 {
		lines = lines[:10]
		lines = append(lines, WrapLines("…", availableWidth)...)
	}

	return strings.Join(lines, "\n")
}

func RenderTool(msg *types.Message, inProgress spinner.Spinner, args, result string, width int, hideToolResults bool) string {
	nameStyle := styles.ToolName
	resultStyle := styles.ToolMessageStyle
	if msg.ToolStatus == types.ToolStatusError {
		nameStyle = styles.ToolNameError
		resultStyle = styles.ToolErrorMessageStyle
	}

	icon := Icon(msg, inProgress)
	name := nameStyle.Render(msg.ToolDefinition.DisplayName())

	if header, ok := RenderFriendlyHeader(msg, inProgress); ok {
		content := header
		if args != "" {
			firstLineWidth := width - lipgloss.Width(content) - 1
			subsequentLineWidth := width - styles.ToolCompletedIcon.GetMarginLeft()
			wrappedArgs := wrapTextWithIndent(args, firstLineWidth, subsequentLineWidth)
			content += " " + wrappedArgs
		}
		if result != "" && !hideToolResults {
			if strings.Count(content, "\n") > 0 || strings.Count(result, "\n") > 0 {
				content += "\n" + resultStyle.MarginLeft(styles.ToolCompletedIcon.GetMarginLeft()).Render(result)
			} else {
				remainingWidth := max(width-lipgloss.Width(content)-1, 1)
				renderedResult := resultStyle.Render(result)
				if lipgloss.Width(renderedResult) > remainingWidth {
					renderedResult = resultStyle.Render(TruncateText(result, remainingWidth))
				}
				content += " " + renderedResult
			}
		}
		return styles.RenderComposite(styles.ToolMessageStyle.Width(width), content)
	}

	content := fmt.Sprintf("%s%s", icon, name)

	if args != "" {
		firstLineWidth := width - lipgloss.Width(content) - 1 // -1 for space before args
		subsequentLineWidth := width - styles.ToolCompletedIcon.GetMarginLeft()
		wrappedArgs := wrapTextWithIndent(args, firstLineWidth, subsequentLineWidth)
		content += " " + wrappedArgs
	}
	if result != "" {
		if strings.Count(content, "\n") > 0 || strings.Count(result, "\n") > 0 {
			if !hideToolResults {
				content += "\n" + resultStyle.MarginLeft(styles.ToolCompletedIcon.GetMarginLeft()).Render(result)
			}
		} else {
			remainingWidth := max(width-lipgloss.Width(content)-1, 1)
			renderedResult := resultStyle.Render(result)
			if lipgloss.Width(renderedResult) > remainingWidth {
				// Truncate result to fit, leaving space for ellipsis
				renderedResult = resultStyle.Render(TruncateText(result, remainingWidth))
			}
			content += " " + renderedResult
		}
	}

	return styles.RenderComposite(styles.ToolMessageStyle.Width(width), content)
}

// ShortenPath replaces home directory with ~ for cleaner display.
func ShortenPath(path string) string {
	if path == "" {
		return path
	}
	homeDir := paths.GetHomeDir()
	if homeDir != "" && strings.HasPrefix(path, homeDir) {
		return "~" + strings.TrimPrefix(path, homeDir)
	}
	return path
}

// RenderFriendlyHeader renders a friendly description header if present in the tool call arguments.
// Returns the rendered header string and true if a friendly description was found, empty string and false otherwise.
// Custom renderers can use this to show the friendly description before their custom content.
func RenderFriendlyHeader(msg *types.Message, s spinner.Spinner) (string, bool) {
	friendlyDesc := tools.ExtractDescription(msg.ToolCall.Function.Arguments)
	if friendlyDesc == "" {
		return "", false
	}

	icon := Icon(msg, s)
	content := fmt.Sprintf("%s %s", icon, styles.ToolDescription.Render(friendlyDesc))
	content += " " + styles.ToolNameDim.Render("("+msg.ToolDefinition.DisplayName()+")")
	return content, true
}
