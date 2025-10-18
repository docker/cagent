package bedrock

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// StreamAdapter adapts the Bedrock stream to chat.MessageStream interface
type StreamAdapter struct {
	events           <-chan types.ResponseStream
	model            string
	modelFamily      string
	toolCalls        map[int]string    // Track tool call IDs
	lastFinishReason chat.FinishReason // Track finish reason to avoid overwriting
}

// newStreamAdapter creates a new stream adapter
func newStreamAdapter(events <-chan types.ResponseStream, model, modelFamily string) *StreamAdapter {
	return &StreamAdapter{
		events:      events,
		model:       model,
		modelFamily: modelFamily,
		toolCalls:   make(map[int]string),
	}
}

// Recv gets the next completion chunk
func (a *StreamAdapter) Recv() (chat.MessageStreamResponse, error) {
	if a.events == nil {
		return chat.MessageStreamResponse{}, io.EOF
	}

	event, ok := <-a.events
	if !ok {
		// Stream closed
		return chat.MessageStreamResponse{}, io.EOF
	}

	// Process event based on type
	switch e := event.(type) {
	case *types.ResponseStreamMemberChunk:
		return a.processChunk(e.Value.Bytes)
	default:
		slog.Warn("Unexpected Bedrock stream event", "type", fmt.Sprintf("%T", e))
		return chat.MessageStreamResponse{}, fmt.Errorf("unexpected stream event: %T", e)
	}
}

// processChunk processes a response chunk based on model family
func (a *StreamAdapter) processChunk(chunk []byte) (chat.MessageStreamResponse, error) {
	switch a.modelFamily {
	case "anthropic":
		return a.processClaudeChunk(chunk)
	case "titan":
		return a.processTitanChunk(chunk)
	case "llama", "mistral":
		return a.processLlamaMistralChunk(chunk)
	default:
		return chat.MessageStreamResponse{}, fmt.Errorf("unsupported model family: %s", a.modelFamily)
	}
}

// processClaudeChunk processes Claude model chunks
func (a *StreamAdapter) processClaudeChunk(chunk []byte) (chat.MessageStreamResponse, error) {
	// Claude chunks follow the Messages API streaming format
	var claudeChunk struct {
		Type  string `json:"type"`
		Index int    `json:"index,omitempty"`
		Delta struct {
			Type         string `json:"type"`
			Text         string `json:"text,omitempty"`
			PartialJSON  string `json:"partial_json,omitempty"`
			StopReason   string `json:"stop_reason,omitempty"`
			StopSequence string `json:"stop_sequence,omitempty"`
		} `json:"delta,omitempty"`
		ContentBlock struct {
			Type  string          `json:"type"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
			Text  string          `json:"text,omitempty"`
		} `json:"content_block,omitempty"`
		Message struct {
			ID           string `json:"id"`
			Type         string `json:"type"`
			Role         string `json:"role"`
			Model        string `json:"model"`
			StopReason   string `json:"stop_reason,omitempty"`
			StopSequence string `json:"stop_sequence,omitempty"`
			Usage        struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage,omitempty"`
		} `json:"message,omitempty"`
	}

	if err := json.Unmarshal(chunk, &claudeChunk); err != nil {
		slog.Error("Failed to parse Claude chunk", "error", err, "chunk", string(chunk))
		return chat.MessageStreamResponse{}, fmt.Errorf("failed to parse chunk: %w", err)
	}

	response := chat.MessageStreamResponse{
		Model: a.model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: claudeChunk.Index,
				Delta: chat.MessageDelta{
					Role: string(chat.MessageRoleAssistant),
				},
			},
		},
	}

	switch claudeChunk.Type {
	case "message_start":
		response.ID = claudeChunk.Message.ID
		if claudeChunk.Message.Usage.InputTokens > 0 || claudeChunk.Message.Usage.OutputTokens > 0 {
			response.Usage = &chat.Usage{
				InputTokens:  claudeChunk.Message.Usage.InputTokens,
				OutputTokens: claudeChunk.Message.Usage.OutputTokens,
			}
		}

	case "content_block_start":
		if claudeChunk.ContentBlock.Type == "tool_use" {
			toolCall := tools.ToolCall{
				ID:   claudeChunk.ContentBlock.ID,
				Type: "function",
				Function: tools.FunctionCall{
					Name: claudeChunk.ContentBlock.Name,
				},
			}
			response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
			a.toolCalls[claudeChunk.Index] = claudeChunk.ContentBlock.ID
		}

	case "content_block_delta":
		if claudeChunk.Delta.Type == "text_delta" {
			response.Choices[0].Delta.Content = claudeChunk.Delta.Text
		} else if claudeChunk.Delta.Type == "input_json_delta" {
			if toolID, ok := a.toolCalls[claudeChunk.Index]; ok {
				toolCall := tools.ToolCall{
					ID:   toolID,
					Type: "function",
					Function: tools.FunctionCall{
						Arguments: claudeChunk.Delta.PartialJSON,
					},
				}
				response.Choices[0].Delta.ToolCalls = []tools.ToolCall{toolCall}
			}
		}

	case "message_delta":
		if claudeChunk.Delta.StopReason != "" {
			finishReason := mapClaudeStopReason(claudeChunk.Delta.StopReason)
			response.Choices[0].FinishReason = finishReason
			// Track the finish reason so message_stop doesn't overwrite it
			a.lastFinishReason = finishReason
		}
		if claudeChunk.Message.Usage.OutputTokens > 0 {
			response.Usage = &chat.Usage{
				OutputTokens: claudeChunk.Message.Usage.OutputTokens,
			}
		}

	case "message_stop":
		// Only set FinishReason to Stop if we haven't already set it
		// (e.g., from tool_use in message_delta)
		if a.lastFinishReason != "" {
			response.Choices[0].FinishReason = a.lastFinishReason
		} else {
			response.Choices[0].FinishReason = chat.FinishReasonStop
		}
	}

	return response, nil
}

// processTitanChunk processes Amazon Titan model chunks
func (a *StreamAdapter) processTitanChunk(chunk []byte) (chat.MessageStreamResponse, error) {
	var titanChunk struct {
		OutputText                string `json:"outputText"`
		Index                     int    `json:"index"`
		CompletionReason          string `json:"completionReason,omitempty"`
		InputTextTokenCount       int    `json:"inputTextTokenCount,omitempty"`
		TotalOutputTextTokenCount int    `json:"totalOutputTextTokenCount,omitempty"`
	}

	if err := json.Unmarshal(chunk, &titanChunk); err != nil {
		slog.Error("Failed to parse Titan chunk", "error", err, "chunk", string(chunk))
		return chat.MessageStreamResponse{}, fmt.Errorf("failed to parse chunk: %w", err)
	}

	response := chat.MessageStreamResponse{
		Model: a.model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: titanChunk.Index,
				Delta: chat.MessageDelta{
					Role:    string(chat.MessageRoleAssistant),
					Content: titanChunk.OutputText,
				},
			},
		},
	}

	if titanChunk.CompletionReason != "" {
		response.Choices[0].FinishReason = mapTitanCompletionReason(titanChunk.CompletionReason)
	}

	if titanChunk.InputTextTokenCount > 0 || titanChunk.TotalOutputTextTokenCount > 0 {
		response.Usage = &chat.Usage{
			InputTokens:  titanChunk.InputTextTokenCount,
			OutputTokens: titanChunk.TotalOutputTextTokenCount,
		}
	}

	return response, nil
}

// processLlamaMistralChunk processes Llama and Mistral model chunks
func (a *StreamAdapter) processLlamaMistralChunk(chunk []byte) (chat.MessageStreamResponse, error) {
	var llamaChunk struct {
		Generation           string `json:"generation"`
		PromptTokenCount     int    `json:"prompt_token_count,omitempty"`
		GenerationTokenCount int    `json:"generation_token_count,omitempty"`
		StopReason           string `json:"stop_reason,omitempty"`
	}

	if err := json.Unmarshal(chunk, &llamaChunk); err != nil {
		slog.Error("Failed to parse Llama/Mistral chunk", "error", err, "chunk", string(chunk))
		return chat.MessageStreamResponse{}, fmt.Errorf("failed to parse chunk: %w", err)
	}

	response := chat.MessageStreamResponse{
		Model: a.model,
		Choices: []chat.MessageStreamChoice{
			{
				Index: 0,
				Delta: chat.MessageDelta{
					Role:    string(chat.MessageRoleAssistant),
					Content: llamaChunk.Generation,
				},
			},
		},
	}

	if llamaChunk.StopReason != "" {
		response.Choices[0].FinishReason = mapLlamaStopReason(llamaChunk.StopReason)
	}

	if llamaChunk.PromptTokenCount > 0 || llamaChunk.GenerationTokenCount > 0 {
		response.Usage = &chat.Usage{
			InputTokens:  llamaChunk.PromptTokenCount,
			OutputTokens: llamaChunk.GenerationTokenCount,
		}
	}

	return response, nil
}

// mapClaudeStopReason maps Claude stop reasons to standard finish reasons
func mapClaudeStopReason(reason string) chat.FinishReason {
	switch reason {
	case "end_turn":
		return chat.FinishReasonStop
	case "max_tokens":
		return chat.FinishReasonLength
	case "tool_use":
		return chat.FinishReasonToolCalls
	case "stop_sequence":
		return chat.FinishReasonStop
	default:
		return chat.FinishReasonStop
	}
}

// mapTitanCompletionReason maps Titan completion reasons to standard finish reasons
func mapTitanCompletionReason(reason string) chat.FinishReason {
	switch reason {
	case "FINISH":
		return chat.FinishReasonStop
	case "LENGTH":
		return chat.FinishReasonLength
	default:
		return chat.FinishReasonStop
	}
}

// mapLlamaStopReason maps Llama/Mistral stop reasons to standard finish reasons
func mapLlamaStopReason(reason string) chat.FinishReason {
	switch reason {
	case "stop":
		return chat.FinishReasonStop
	case "length":
		return chat.FinishReasonLength
	default:
		return chat.FinishReasonStop
	}
}

// Close closes the stream
func (a *StreamAdapter) Close() {
	// The event channel will be closed by the SDK when the stream ends
	// We don't need to do anything here
}
