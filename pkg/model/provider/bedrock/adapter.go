package bedrock

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// StreamAdapter adapts the Bedrock Converse stream to chat.MessageStream interface
type StreamAdapter struct {
	stream       *bedrockruntime.ConverseStreamOutput
	model        string
	eventStream  <-chan types.ConverseStreamOutput
	toolCallData map[int]*toolCallInfo // Track tool call data by index
}

// toolCallInfo holds information about a tool call being streamed
type toolCallInfo struct {
	ID        string
	Name      string
	Arguments string
}

// newStreamAdapter creates a new stream adapter for Converse API
func newStreamAdapter(output *bedrockruntime.ConverseStreamOutput, model string) *StreamAdapter {
	return &StreamAdapter{
		stream:       output,
		model:        model,
		eventStream:  output.GetStream().Events(),
		toolCallData: make(map[int]*toolCallInfo),
	}
}

// Recv gets the next completion chunk from the Converse API stream
func (a *StreamAdapter) Recv() (chat.MessageStreamResponse, error) {
	if a.eventStream == nil {
		return chat.MessageStreamResponse{}, io.EOF
	}

	event, ok := <-a.eventStream
	if !ok {
		// Stream closed
		return chat.MessageStreamResponse{}, io.EOF
	}

	return a.processConverseEvent(event)
}

// processConverseEvent processes a Converse API stream event
func (a *StreamAdapter) processConverseEvent(event types.ConverseStreamOutput) (chat.MessageStreamResponse, error) {
	response := chat.MessageStreamResponse{
		Model: a.model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: 0,
				Delta: chat.MessageDelta{
					Role: string(chat.MessageRoleAssistant),
				},
			},
		},
	}

	switch e := event.(type) {
	case *types.ConverseStreamOutputMemberMessageStart:
		// Message start event - provides role
		slog.Debug("Converse MessageStart event", "role", e.Value.Role)
		return response, nil

	case *types.ConverseStreamOutputMemberContentBlockStart:
		// Content block start - may be text or tool use
		if e.Value.Start != nil {
			switch start := e.Value.Start.(type) {
			case *types.ContentBlockStartMemberToolUse:
				// Tool use started - check for nil values before dereferencing
				if start.Value.ToolUseId == nil || start.Value.Name == nil {
					slog.Warn("Converse ContentBlockStart (ToolUse) missing required fields",
						"tool_use_id_nil", start.Value.ToolUseId == nil,
						"name_nil", start.Value.Name == nil)
					return response, nil
				}

				toolCall := tools.ToolCall{
					ID:   *start.Value.ToolUseId,
					Type: "function",
					Function: tools.FunctionCall{
						Name: *start.Value.Name,
					},
				}
				response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}

				// Store tool call info for delta events
				if e.Value.ContentBlockIndex != nil {
					a.toolCallData[int(*e.Value.ContentBlockIndex)] = &toolCallInfo{
						ID:   *start.Value.ToolUseId,
						Name: *start.Value.Name,
					}
				}

				slog.Debug("Converse ContentBlockStart (ToolUse)",
					"tool_id", *start.Value.ToolUseId,
					"tool_name", *start.Value.Name,
					"index", e.Value.ContentBlockIndex)
			}
		}
		return response, nil

	case *types.ConverseStreamOutputMemberContentBlockDelta:
		// Content block delta - streaming content
		if e.Value.Delta != nil {
			switch delta := e.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				// Text content delta
				response.Choices[0].Delta.Content = delta.Value
				slog.Debug("Converse ContentBlockDelta (Text)", "length", len(delta.Value))

			case *types.ContentBlockDeltaMemberToolUse:
				// Tool use input delta (streaming JSON arguments)
				// Accumulate but DON'T send to runtime until complete
				if e.Value.ContentBlockIndex != nil && delta.Value.Input != nil {
					if toolInfo, ok := a.toolCallData[int(*e.Value.ContentBlockIndex)]; ok {
						toolInfo.Arguments += *delta.Value.Input
						slog.Debug("Converse ContentBlockDelta (ToolUse) accumulated",
							"chunk_length", len(*delta.Value.Input),
							"total_length", len(toolInfo.Arguments))
					}
				}
			}
		}
		return response, nil

	case *types.ConverseStreamOutputMemberContentBlockStop:
		// Content block stopped - now send complete tool call if this was a tool use block
		if e.Value.ContentBlockIndex != nil {
			if toolInfo, ok := a.toolCallData[int(*e.Value.ContentBlockIndex)]; ok {
				// Ensure arguments is valid JSON - if empty, use empty object
				args := toolInfo.Arguments
				if args == "" {
					args = "{}"
				}

				// Send the complete tool call now
				toolCall := tools.ToolCall{
					ID:   toolInfo.ID,
					Type: "function",
					Function: tools.FunctionCall{
						Name:      toolInfo.Name,
						Arguments: args,
					},
				}
				response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
				slog.Debug("Converse ContentBlockStop - sending complete tool call",
					"tool_id", toolInfo.ID,
					"tool_name", toolInfo.Name,
					"args_length", len(args))
			}
		}
		slog.Debug("Converse ContentBlockStop", "index", e.Value.ContentBlockIndex)
		return response, nil

	case *types.ConverseStreamOutputMemberMessageStop:
		// Message stopped - provides stop reason
		if e.Value.StopReason != "" {
			response.Choices[0].FinishReason = mapConverseStopReason(e.Value.StopReason)
			slog.Debug("Converse MessageStop", "stop_reason", e.Value.StopReason)
		}
		return response, nil

	case *types.ConverseStreamOutputMemberMetadata:
		// Metadata event - provides token usage
		if e.Value.Usage != nil {
			usage := &chat.Usage{}
			if e.Value.Usage.InputTokens != nil {
				usage.InputTokens = int64(*e.Value.Usage.InputTokens)
			}
			if e.Value.Usage.OutputTokens != nil {
				usage.OutputTokens = int64(*e.Value.Usage.OutputTokens)
			}
			response.Usage = usage
			slog.Debug("Converse Metadata", "input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens)
		}
		return response, nil

	default:
		slog.Warn("Unexpected Converse stream event", "type", fmt.Sprintf("%T", event))
		return chat.MessageStreamResponse{}, fmt.Errorf("unexpected stream event: %T", event)
	}
}

// mapConverseStopReason maps Converse API stop reasons to standard finish reasons
func mapConverseStopReason(reason types.StopReason) chat.FinishReason {
	switch reason {
	case types.StopReasonEndTurn:
		return chat.FinishReasonStop
	case types.StopReasonMaxTokens:
		return chat.FinishReasonLength
	case types.StopReasonToolUse:
		return chat.FinishReasonToolCalls
	case types.StopReasonStopSequence:
		return chat.FinishReasonStop
	case types.StopReasonContentFiltered:
		return chat.FinishReasonContentFilter
	default:
		slog.Warn("Unknown stop reason", "reason", reason)
		return chat.FinishReasonStop
	}
}

// Close closes the stream
func (a *StreamAdapter) Close() {
	// The event channel will be closed by the SDK when the stream ends
	// We don't need to do anything here
}
