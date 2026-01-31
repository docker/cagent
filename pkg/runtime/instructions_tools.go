package runtime

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// Matches instruction tool placeholders inside SYSTEM messages.
// Example:
//
//	{{ tool:read_file agents.md }}
//
// Captures:
//  1. tool name
//  2. raw arguments string
var toolCallRegex = regexp.MustCompile(
	`\{\{\s*tool:([a-zA-Z0-9_\-]+)\s+([^}]+)\s*\}\}`,
)

// ResolveInstructionToolCalls scans SYSTEM messages for instruction-level
// tool placeholders and converts them into regular assistant tool calls.
//
// This approach intentionally does NOT execute tools inline.
// Instead, it injects tool calls into the normal runtime flow so that
// approvals, MCP tools, resume behavior, and max_iterations limits
// remain fully enforced.
func (r *LocalRuntime) ResolveInstructionToolCalls(
	ctx context.Context,
	messages []chat.Message,
) ([]chat.Message, error) {
	out := make([]chat.Message, 0, len(messages))

	for _, msg := range messages {
		// Only SYSTEM messages are allowed to contain instruction tool calls.
		// This prevents user or assistant prompt injection.
		if msg.Role != chat.MessageRoleSystem {
			out = append(out, msg)
			continue
		}

		content := msg.Content
		matches := toolCallRegex.FindAllStringSubmatch(content, -1)

		// Fast path: no instruction tools found.
		if len(matches) == 0 {
			out = append(out, msg)
			continue
		}

		var toolCalls []tools.ToolCall

		for _, m := range matches {
			full := m[0]                       // Full placeholder text
			toolName := m[1]                   // Tool name
			rawArgs := strings.TrimSpace(m[2]) // Raw argument string

			// Encode arguments using the same structure expected by
			// standard OpenAI-style function tool calls.
			argsJSON, err := json.Marshal(map[string]string{
				"input": rawArgs,
			})
			if err != nil {
				return nil, err
			}

			// Create a normal tool call that will be processed by the runtime
			// in the usual assistant tool execution phase.
			toolCalls = append(toolCalls, tools.ToolCall{
				ID:   "instruction-" + toolName,
				Type: "function",
				Function: tools.FunctionCall{
					Name:      toolName,
					Arguments: string(argsJSON),
				},
			})

			// Remove the placeholder from the SYSTEM message content.
			// The actual tool execution result will be injected later
			// through the normal tool response mechanism.
			content = strings.ReplaceAll(content, full, "")
		}

		// Emit the cleaned SYSTEM message without tool placeholders.
		msg.Content = strings.TrimSpace(content)

		// Inject an ASSISTANT message containing the generated tool calls.
		// These will be executed by the standard runtime loop.
		out = append(out,
			msg,
			chat.Message{
				Role:      chat.MessageRoleAssistant,
				ToolCalls: toolCalls,
			},
		)
	}

	return out, nil
}
