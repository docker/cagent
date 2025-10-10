package anthropic

import (
	"context"
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
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.config.Model),
		MaxTokens: maxTokens,
		Messages:  convertBetaMessages(messages),
		Tools:     convertBetaTools(requestTools),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaInterleavedThinking2025_05_14},
	}

	// Populate proper Anthropic system prompt from input messages
	if sys := extractBetaSystemBlocks(messages); len(sys) > 0 {
		params.System = sys
	}

	// For interleaved thinking to make sense, we use a default of 16384 tokens for the thinking budget
	thinkingTokens := int64(16384)
	if c.config.ThinkingBudget != nil {
		thinkingTokens = int64(c.config.ThinkingBudget.Tokens)
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
	slog.Debug("Anthropic Beta API chat completion stream created successfully", "model", c.config.Model)

	return newBetaStreamAdapter(stream), nil
}
