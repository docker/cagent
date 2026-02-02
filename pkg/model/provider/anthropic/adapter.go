package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/anthropics/anthropic-sdk-go/shared"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// streamAdapter adapts the Anthropic stream to our interface
type streamAdapter struct {
	stream     *ssestream.Stream[anthropic.MessageStreamEventUnion]
	trackUsage bool
	toolCall   bool
	toolID     string
	// For single retry on context length error
	retryFn            func() *streamAdapter
	retried            bool
	getResponseTrailer func() http.Header
}

func (c *Client) newStreamAdapter(stream *ssestream.Stream[anthropic.MessageStreamEventUnion], trackUsage bool) *streamAdapter {
	return &streamAdapter{
		stream:             stream,
		trackUsage:         trackUsage,
		getResponseTrailer: c.getResponseTrailer,
	}
}

// isContextLengthError checks if the error indicates context window exceeded.
// Anthropic returns HTTP 400 with type "invalid_request_error" for context length issues.
// Unfortunately there's no specific error code - we must check the message.
func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}

	// Parse the error response to get the structured error object
	var errResp struct {
		Error shared.ErrorObjectUnion `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.RawJSON()), &errResp) != nil {
		return false
	}

	// Check if it's an invalid_request_error with a context-length message
	if errResp.Error.Type != "invalid_request_error" {
		return false
	}

	msg := errResp.Error.Message
	return strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "context length") ||
		strings.Contains(msg, "maximum context")
}

// Recv gets the next completion chunk
func (a *streamAdapter) Recv() (chat.MessageStreamResponse, error) {
	if !a.stream.Next() {
		err := a.stream.Err()
		// Single retry on context length error
		if err != nil && !a.retried && a.retryFn != nil && isContextLengthError(err) {
			a.retried = true
			if retry := a.retryFn(); retry != nil {
				a.stream.Close()
				a.stream = retry.stream
				return a.Recv()
			}
		}
		if err != nil {
			return chat.MessageStreamResponse{}, err
		}
		return chat.MessageStreamResponse{}, io.EOF
	}

	event := a.stream.Current()

	response := chat.MessageStreamResponse{
		ID:     event.Message.ID,
		Object: "chat.completion.chunk",
		Model:  string(event.Message.Model),
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
	case anthropic.ContentBlockStartEvent:
		switch block := eventVariant.ContentBlock.AsAny().(type) {
		case anthropic.ToolUseBlock:
			a.toolID = block.ID
			a.toolCall = true
			toolCall := tools.ToolCall{
				ID:   a.toolID,
				Type: "function",
				Function: tools.FunctionCall{
					Name: block.Name,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
		case anthropic.ThinkingBlock:
			// Emit initial thinking content and signature
			if block.Thinking != "" {
				response.Choices[0].Delta.ReasoningContent = block.Thinking
			}
			if block.Signature != "" {
				response.Choices[0].Delta.ThinkingSignature = block.Signature
			}
		}
	case anthropic.ContentBlockDeltaEvent:
		switch deltaVariant := eventVariant.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			response.Choices[0].Delta.Content = deltaVariant.Text
		case anthropic.ThinkingDelta:
			response.Choices[0].Delta.ReasoningContent = deltaVariant.Thinking
		case anthropic.SignatureDelta:
			response.Choices[0].Delta.ThinkingSignature = deltaVariant.Signature
		case anthropic.InputJSONDelta:
			inputBytes := deltaVariant.PartialJSON
			toolCall := tools.ToolCall{
				ID:   a.toolID,
				Type: "function",
				Function: tools.FunctionCall{
					Arguments: inputBytes,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}

		default:
			return response, fmt.Errorf("unknown delta type: %T", deltaVariant)
		}
	case anthropic.MessageDeltaEvent:
		if a.trackUsage {
			response.Usage = &chat.Usage{
				InputTokens:       eventVariant.Usage.InputTokens,
				OutputTokens:      eventVariant.Usage.OutputTokens,
				CachedInputTokens: eventVariant.Usage.CacheReadInputTokens,
				CacheWriteTokens:  eventVariant.Usage.CacheCreationInputTokens,
			}
		}
	case anthropic.MessageStopEvent:
		if a.toolCall {
			response.Choices[0].FinishReason = chat.FinishReasonToolCalls
		} else {
			response.Choices[0].FinishReason = chat.FinishReasonStop
		}

		// MessageStopEvent is the last event. Let's drain the response to get the trailing headers.
		trailers := a.getResponseTrailer()
		if trailers.Get("X-RateLimit-Limit") != "" {
			response.RateLimit = &chat.RateLimit{
				Limit:      parseHeaderInt64(trailers.Get("X-RateLimit-Limit")),
				Remaining:  parseHeaderInt64(trailers.Get("X-RateLimit-Remaining")),
				Reset:      parseHeaderInt64(trailers.Get("X-RateLimit-Reset")),
				RetryAfter: parseHeaderInt64(trailers.Get("Retry-After")),
			}
		}
	}

	return response, nil
}

func parseHeaderInt64(headerValue string) int64 {
	value, err := strconv.ParseInt(headerValue, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// Close closes the stream
func (a *streamAdapter) Close() {
	if a.stream != nil {
		a.stream.Close()
	}
}
