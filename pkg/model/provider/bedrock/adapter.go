package bedrock

import (
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// streamAdapter adapts Bedrock's ConverseStreamEventStream to chat.MessageStream
type streamAdapter struct {
	stream     *bedrockruntime.ConverseStreamEventStream
	model      string
	trackUsage bool

	// State for accumulating tool call data
	currentToolID   string
	currentToolName string
}

func newStreamAdapter(stream *bedrockruntime.ConverseStreamEventStream, model string, trackUsage bool) *streamAdapter {
	return &streamAdapter{
		stream:     stream,
		model:      model,
		trackUsage: trackUsage,
	}
}

// Recv gets the next completion chunk
func (a *streamAdapter) Recv() (chat.MessageStreamResponse, error) {
	event, ok := <-a.stream.Events()
	if !ok {
		// Check for errors
		if err := a.stream.Err(); err != nil {
			return chat.MessageStreamResponse{}, err
		}
		return chat.MessageStreamResponse{}, io.EOF
	}

	response := chat.MessageStreamResponse{
		Object: "chat.completion.chunk",
		Model:  a.model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: 0,
				Delta: chat.MessageDelta{
					Role: string(chat.MessageRoleAssistant),
				},
			},
		},
	}

	switch ev := event.(type) {
	case *types.ConverseStreamOutputMemberMessageStart:
		slog.Debug("Bedrock stream: message start", "role", ev.Value.Role)

	case *types.ConverseStreamOutputMemberContentBlockStart:
		// Handle content block start - tool use or text
		if start, ok := ev.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
			a.currentToolID = derefString(start.Value.ToolUseId)
			a.currentToolName = derefString(start.Value.Name)

			// Emit initial tool call
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{{
				ID:   a.currentToolID,
				Type: "function",
				Function: tools.FunctionCall{
					Name: a.currentToolName,
				},
			}}
		}

	case *types.ConverseStreamOutputMemberContentBlockDelta:
		// Handle content block delta - text or tool input
		if ev.Value.Delta != nil {
			switch delta := ev.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				response.Choices[0].Delta.Content = delta.Value

			case *types.ContentBlockDeltaMemberToolUse:
				// Emit partial tool call with input delta
				response.Choices[0].Delta.ToolCalls = []tools.ToolCall{{
					ID:   a.currentToolID,
					Type: "function",
					Function: tools.FunctionCall{
						Arguments: derefString(delta.Value.Input),
					},
				}}

			case *types.ContentBlockDeltaMemberReasoningContent:
				// Handle reasoning/thinking content
				if textDelta, ok := delta.Value.(*types.ReasoningContentBlockDeltaMemberText); ok {
					response.Choices[0].Delta.ReasoningContent = textDelta.Value
				}
			}
		}

	case *types.ConverseStreamOutputMemberContentBlockStop:
		slog.Debug("Bedrock stream: content block stop", "index", ev.Value.ContentBlockIndex)

	case *types.ConverseStreamOutputMemberMessageStop:
		// Message complete - determine finish reason
		stopReason := ev.Value.StopReason
		switch stopReason {
		case types.StopReasonToolUse:
			response.Choices[0].FinishReason = chat.FinishReasonToolCalls
		case types.StopReasonEndTurn, types.StopReasonStopSequence:
			response.Choices[0].FinishReason = chat.FinishReasonStop
		case types.StopReasonMaxTokens:
			response.Choices[0].FinishReason = chat.FinishReasonLength
		default:
			response.Choices[0].FinishReason = chat.FinishReasonStop
		}

	case *types.ConverseStreamOutputMemberMetadata:
		// Metadata event with usage info
		if a.trackUsage && ev.Value.Usage != nil {
			response.Usage = &chat.Usage{
				InputTokens:  int64(derefInt32(ev.Value.Usage.InputTokens)),
				OutputTokens: int64(derefInt32(ev.Value.Usage.OutputTokens)),
			}
		}
	}

	return response, nil
}

// Close closes the stream
func (a *streamAdapter) Close() {
	if a.stream != nil {
		_ = a.stream.Close()
	}
}

// derefString safely dereferences a string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt32 safely dereferences an int32 pointer
func derefInt32(i *int32) int32 {
	if i == nil {
		return 0
	}
	return *i
}
