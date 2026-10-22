package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider/base"
	"github.com/docker/docker-agent/pkg/model/provider/contracts"
	"github.com/docker/docker-agent/pkg/tools"
)

// betaStreamAdapter adapts the Anthropic Beta stream to our interface
type betaStreamAdapter struct {
	retryableStream[anthropic.BetaRawMessageStreamEventUnion]

	trackUsage bool
	toolCall   bool
	stopReason anthropic.BetaStopReason
	// toolIDByBlock maps a content block index to its tool_use block ID.
	// See streamAdapter.toolIDByBlock for the same rationale (parallel
	// tool calls require per-block routing of input_json deltas).
	toolIDByBlock map[int64]string
	// message/rawLost: see streamAdapter.
	message        anthropic.BetaMessage
	rawLost        bool
	requestContext json.RawMessage
	// config is the dispatching client's; served is the model a server
	// fallback handed the turn to, unset when the dispatched model served it.
	// The runtime may read it while the reader goroutine still writes it
	// (cancellation, idle timeout).
	config base.Config
	served atomic.Pointer[string]
}

// newBetaStreamAdapter creates a new Beta stream adapter
func (c *Client) newBetaStreamAdapter(stream *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion], trackUsage bool) *betaStreamAdapter {
	return &betaStreamAdapter{
		retryableStream: retryableStream[anthropic.BetaRawMessageStreamEventUnion]{stream: stream},
		trackUsage:      trackUsage,
		toolIDByBlock:   map[int64]string{},
		config:          c.Config,
	}
}

// Recv gets the next completion chunk from the Beta stream
func (a *betaStreamAdapter) Recv() (chat.MessageStreamResponse, error) {
	ok, err := a.next()
	if !ok {
		return chat.MessageStreamResponse{}, wrapAnthropicError(err)
	}

	event := a.stream.Current()
	a.accumulate(event)

	response := chat.MessageStreamResponse{
		ID:     event.Message.ID,
		Object: "chat.completion.chunk",
		Model:  event.Message.Model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: 0,
				Delta: chat.MessageDelta{
					Role: string(chat.MessageRoleAssistant),
				},
			},
		},
	}

	// Handle different event types
	switch eventVariant := event.AsAny().(type) {
	case anthropic.BetaRawContentBlockStartEvent:
		switch block := eventVariant.ContentBlock.AsAny().(type) {
		case anthropic.BetaToolUseBlock:
			a.toolIDByBlock[eventVariant.Index] = block.ID
			a.toolCall = true
			toolCall := tools.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: tools.FunctionCall{
					Name: block.Name,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
		case anthropic.BetaFallbackBlock:
			// The declined model's tool calls are superseded.
			a.toolCall = false
			a.served.Store(&block.To.Model)
			response.Choices[0].Delta.ServerFallback = &chat.ServerFallback{From: block.From.Model, To: block.To.Model}
		case anthropic.BetaThinkingBlock:
			if block.Thinking != "" {
				response.Choices[0].Delta.ReasoningContent = block.Thinking
				slog.Debug("Received thinking", "thinking", block.Thinking)
			}
			if block.Signature != "" {
				response.Choices[0].Delta.ThinkingSignature = block.Signature
			}
		}
	case anthropic.BetaRawContentBlockDeltaEvent:
		switch deltaVariant := eventVariant.Delta.AsAny().(type) {
		case anthropic.BetaTextDelta:
			response.Choices[0].Delta.Content = deltaVariant.Text
		case anthropic.BetaThinkingDelta:
			response.Choices[0].Delta.ReasoningContent = deltaVariant.Thinking
		case anthropic.BetaInputJSONDelta:
			inputBytes := deltaVariant.PartialJSON
			toolCall := tools.ToolCall{
				ID:   a.toolIDByBlock[eventVariant.Index],
				Type: "function",
				Function: tools.FunctionCall{
					Arguments: inputBytes,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
		case anthropic.BetaSignatureDelta:
			// Signature delta is for thinking blocks - capture it so we can replay thinking in history
			response.Choices[0].Delta.ThinkingSignature = deltaVariant.Signature
		default:
			return response, fmt.Errorf("unknown delta type: %T", deltaVariant)
		}
	case anthropic.BetaRawMessageDeltaEvent:
		a.stopReason = eventVariant.Delta.StopReason
		// Sticky routing sends no marker: only a fallback_message iteration
		// shows that a fallback model served the turn.
		if a.served.Load() == nil {
			for _, it := range slices.Backward(a.message.Usage.Iterations) {
				if it.Type == "fallback_message" {
					a.served.Store(&it.Model)
					break
				}
			}
		}
		if a.trackUsage {
			response.Usage = betaUsageFromMessage(a.message.Usage)
		}
	case anthropic.BetaRawMessageStopEvent:
		for _, dropped := range thinkingTransformations(a.message.InputTransformations) {
			slog.Warn("Anthropic dropped invalidated thinking", "block", dropped)
		}
		response.Choices[0].FinishReason = finishReason(anthropic.StopReason(a.stopReason), a.toolCall)
		if !a.rawLost {
			response.Choices[0].Delta.ProviderState = newProviderState(a.message.ID, a.message.Model, a.message.Content)
		}
		state := withRequestContext(response.Choices[0].Delta.ProviderState, a.requestContext)
		diagnosis := cacheDiagnostics(a.message.Diagnostics)
		if state == nil && diagnosis != nil {
			state = &chat.ProviderState{Provider: providerStateName, MessageID: a.message.ID, Content: json.RawMessage("[]")}
		}
		if state != nil {
			state.CacheDiagnostics = diagnosis
		}
		response.Choices[0].Delta.ProviderState = state
	}

	return response, nil
}

func (a *betaStreamAdapter) accumulate(event anthropic.BetaRawMessageStreamEventUnion) {
	// Usage must keep accumulating even if raw content capture failed.
	if a.rawLost && event.Type != "message_start" && event.Type != "message_delta" {
		return
	}
	if err := a.message.Accumulate(event); err != nil {
		a.rawLost = true
		slog.Debug("Anthropic Beta: raw response capture disabled", "error", err)
	}
}

// ServedBy reports the model a server fallback handed the turn to.
func (a *betaStreamAdapter) ServedBy() (contracts.ServingModel, bool) {
	served := a.served.Load()
	if served == nil {
		return contracts.ServingModel{}, false
	}
	cfg := a.config
	cfg.ModelConfig.Model = *served
	// The primary's display name and cost overrides don't describe the
	// fallback model, which is priced from the catalogue.
	cfg.ModelConfig.DisplayModel = ""
	cfg.ModelConfig.Cost = nil
	return contracts.ServingModel{ID: cfg.ID(), Config: cfg}, true
}

// Close closes the Beta stream
func (a *betaStreamAdapter) Close() {
	a.close()
}

// betaUsageFromMessage maps accumulated usage; reasoning is already included in output.
func betaUsageFromMessage(u anthropic.BetaUsage) *chat.Usage {
	return &chat.Usage{
		InputTokens:       u.InputTokens,
		OutputTokens:      u.OutputTokens,
		CachedInputTokens: u.CacheReadInputTokens,
		CacheWriteTokens:  u.CacheCreationInputTokens,
		ReasoningTokens:   u.OutputTokensDetails.ThinkingTokens,
	}
}
