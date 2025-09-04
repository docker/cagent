package anthropic

import (
	"fmt"
	"io"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// streamAdapter adapts the Anthropic stream to our interface
type streamAdapter struct {
	stream   *ssestream.Stream[anthropic.MessageStreamEventUnion]
	toolCall bool
	toolID   string
}

func newStreamAdapter(stream *ssestream.Stream[anthropic.MessageStreamEventUnion]) *streamAdapter {
	return &streamAdapter{
		stream: stream,
	}
}

// Recv gets the next completion chunk
func (a *streamAdapter) Recv() (chat.MessageStreamResponse, error) {
	if !a.stream.Next() {
		if a.stream.Err() != nil {
			return chat.MessageStreamResponse{}, a.stream.Err()
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
		if contentBlock, ok := eventVariant.ContentBlock.AsAny().(anthropic.ToolUseBlock); ok {
			a.toolID = contentBlock.ID
			a.toolCall = true
			toolCall := tools.ToolCall{
				ID:   a.toolID,
				Type: "function",
				Function: tools.FunctionCall{
					Name: contentBlock.Name,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
		}
	case anthropic.ContentBlockDeltaEvent:
		switch deltaVariant := eventVariant.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			response.Choices[0].Delta.Content = deltaVariant.Text
		case anthropic.ThinkingDelta:
			response.Choices[0].Delta.ReasoningContent = deltaVariant.Thinking
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
		response.Usage = &chat.Usage{
			InputTokens:        int(eventVariant.Usage.InputTokens),
			OutputTokens:       int(eventVariant.Usage.OutputTokens),
			CachedInputTokens:  int(eventVariant.Usage.CacheReadInputTokens),
			CachedOutputTokens: int(eventVariant.Usage.CacheCreationInputTokens),
		}
	case anthropic.MessageStopEvent:
		if a.toolCall {
			response.Choices[0].FinishReason = chat.FinishReasonToolCalls
		} else {
			response.Choices[0].FinishReason = chat.FinishReasonStop
		}
	}

	return response, nil
}

// Close closes the stream
func (a *streamAdapter) Close() {
	if a.stream != nil {
		a.stream.Close()
	}
}
