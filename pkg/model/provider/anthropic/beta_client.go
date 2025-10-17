package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// createBetaStream creates a streaming chat completion using the Beta Messages API
// This is used when extended thinking is enabled via thinking_budget
func (c *Client) createBetaStream(
	ctx context.Context,
	client anthropic.Client,
	messages []chat.Message,
	requestTools []tools.Tool,
	maxTokens int64,
) (chat.MessageStream, error) {
	allTools, err := convertBetaTools(requestTools)
	if err != nil {
		slog.Error("Failed to convert tools for Anthropic Beta request", "error", err)
		return nil, err
	}

	converted := convertBetaMessages(messages)
	if err := validateAnthropicSequencingBeta(converted); err != nil {
		slog.Warn("Invalid message sequencing for Anthropic Beta API detected, attempting self-repair", "error", err)
		converted = repairAnthropicSequencingBeta(converted)
		if err2 := validateAnthropicSequencingBeta(converted); err2 != nil {
			slog.Error("Failed to self-repair Anthropic Beta sequencing", "error", err2)
			return nil, err
		}
	}

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.ModelConfig.Model),
		MaxTokens: maxTokens,
		Messages:  converted,
		Tools:     allTools,
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaInterleavedThinking2025_05_14},
	}

	// Populate proper Anthropic system prompt from input messages
	if sys := extractBetaSystemBlocks(messages); len(sys) > 0 {
		params.System = sys
	}

	// For interleaved thinking to make sense, we use a default of 16384 tokens for the thinking budget
	thinkingTokens := int64(16384)
	if c.ModelConfig.ThinkingBudget != nil {
		thinkingTokens = int64(c.ModelConfig.ThinkingBudget.Tokens)
	} else {
		slog.Info("Anthropic Beta API using default thinking_budget with interleaved thinking", "budget_tokens", thinkingTokens)
	}
	switch {
	case thinkingTokens >= 1024 && thinkingTokens < maxTokens:
		params.Thinking = anthropic.BetaThinkingConfigParamOfEnabled(thinkingTokens)
		slog.Debug("Anthropic Beta API using thinking_budget with interleaved thinking", "budget_tokens", thinkingTokens)
	case thinkingTokens >= maxTokens:
		slog.Warn("Anthropic Beta API thinking_budget must be less than max_tokens, ignoring", "tokens", thinkingTokens, "max_tokens", maxTokens)
	default:
		slog.Warn("Anthropic Beta API thinking_budget below minimum (1024), ignoring", "tokens", thinkingTokens)
	}

	if len(requestTools) > 0 {
		slog.Debug("Anthropic Beta API: Adding tools to request", "tool_count", len(requestTools))
	}

	slog.Debug("Anthropic Beta API chat completion stream request",
		"model", params.Model,
		"max_tokens", maxTokens,
		"message_count", len(params.Messages))

	stream := client.Beta.Messages.NewStreaming(ctx, params)
	slog.Debug("Anthropic Beta API chat completion stream created successfully", "model", c.ModelConfig.Model)

	return newBetaStreamAdapter(stream), nil
}

// validateAnthropicSequencingBeta performs the same validation as standard API but for Beta payloads
func validateAnthropicSequencingBeta(msgs []anthropic.BetaMessageParam) error {
	for i := range msgs {
		m, ok := marshalToMapBeta(msgs[i])
		if !ok || m["role"] != "assistant" {
			continue
		}

		toolUseIDs := collectToolUseIDs(contentArrayBeta(m))
		if len(toolUseIDs) == 0 {
			continue
		}

		if i+1 >= len(msgs) {
			slog.Warn("Anthropic (beta) sequencing invalid: assistant tool_use present but no next user tool_result message", "assistant_index", i)
			return errors.New("assistant tool_use present but no subsequent user message with tool_result blocks (beta)")
		}

		next, ok := marshalToMapBeta(msgs[i+1])
		if !ok || next["role"] != "user" {
			slog.Warn("Anthropic (beta) sequencing invalid: next message after assistant tool_use is not user", "assistant_index", i, "next_role", next["role"])
			return errors.New("assistant tool_use must be followed by a user message containing corresponding tool_result blocks (beta)")
		}

		toolResultIDs := collectToolResultIDs(contentArrayBeta(next))
		missing := differenceIDs(toolUseIDs, toolResultIDs)
		if len(missing) > 0 {
			slog.Warn("Anthropic (beta) sequencing invalid: missing tool_result for tool_use id in next user message", "assistant_index", i, "tool_use_id", missing[0], "missing_count", len(missing))
			return fmt.Errorf("missing tool_result for tool_use id %s in the next user message (beta)", missing[0])
		}
	}
	return nil
}

// repairAnthropicSequencingBeta inserts a synthetic user message with tool_result blocks
// for any assistant tool_use blocks that don't have corresponding tool_result blocks
// in the immediate next user message.
func repairAnthropicSequencingBeta(msgs []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	if len(msgs) == 0 {
		return msgs
	}
	repaired := make([]anthropic.BetaMessageParam, 0, len(msgs)+2)
	for i := range msgs {
		repaired = append(repaired, msgs[i])

		m, ok := marshalToMapBeta(msgs[i])
		if !ok || m["role"] != "assistant" {
			continue
		}

		toolUseIDs := collectToolUseIDs(contentArrayBeta(m))
		if len(toolUseIDs) == 0 {
			continue
		}

		if i+1 < len(msgs) {
			if next, ok := marshalToMapBeta(msgs[i+1]); ok && next["role"] == "user" {
				toolResultIDs := collectToolResultIDs(contentArrayBeta(next))
				for id := range toolResultIDs {
					delete(toolUseIDs, id)
				}
			}
		}

		if len(toolUseIDs) > 0 {
			blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolUseIDs))
			for id := range toolUseIDs {
				blocks = append(blocks, anthropic.BetaContentBlockParamUnion{
					OfToolResult: &anthropic.BetaToolResultBlockParam{
						ToolUseID: id,
						Content: []anthropic.BetaToolResultBlockParamContentUnion{
							{OfText: &anthropic.BetaTextBlockParam{Text: "(tool execution failed)"}},
						},
					},
				})
			}
			repaired = append(repaired, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleUser,
				Content: blocks,
			})
		}
	}
	return repaired
}

func marshalToMapBeta(v any) (map[string]any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	return m, true
}

func contentArrayBeta(m map[string]any) []any {
	if a, ok := m["content"].([]any); ok {
		return a
	}
	return nil
}
