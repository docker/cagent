package gemini

import (
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// StreamAdapter adapts the Gemini streaming iterator to chat.MessageStream
type StreamAdapter struct {
	ch           chan result
	model        string
	mu           sync.Mutex
	lastResponse *genai.GenerateContentResponse // Store last response for final message
}

type result struct {
	resp *genai.GenerateContentResponse
	err  error
	done bool
}

// NewStreamAdapter constructs a StreamAdapter from Gemini's iterator
func NewStreamAdapter(iter func(func(*genai.GenerateContentResponse, error) bool), model string) *StreamAdapter {
	adapter := &StreamAdapter{
		ch:    make(chan result),
		model: model,
	}

	go func() {
		defer close(adapter.ch)

		hasContent := false
		hasToolCalls := false

		// Consume the iterator
		iter(func(resp *genai.GenerateContentResponse, err error) bool {
			// Skip noisy http2 errors
			if err != nil && err.Error() == "http2: response body closed" {
				return true
			}

			// Handle streaming parser errors from new Gemini 2.5 response fields
			if err != nil {
				errMsg := err.Error()
				// Check if this is a streaming chunk parsing error that contains valid response data
				if strings.Contains(errMsg, "invalid stream chunk") && strings.Contains(errMsg, `"text":`) {
					// Try to extract text content from the error message
					if textContent := extractTextFromError(errMsg); textContent != "" {
						// Create a synthetic response with the extracted text
						adapter.ch <- result{resp: &genai.GenerateContentResponse{
							Candidates: []*genai.Candidate{
								{
									Content: &genai.Content{
										Parts: []*genai.Part{
											{Text: textContent},
										},
									},
								},
							},
						}}
						hasContent = true

						// Check if this appears to be a complete response (has finishReason)
						if strings.Contains(errMsg, `"finishReason"`) {
							// This is the final chunk, send done signal
							adapter.ch <- result{done: true}
							return false
						}

						// Continue iteration to potentially get more chunks
						return true
					}
				}

				adapter.ch <- result{err: err}
				return false
			}

			if resp != nil {
				// Check for text content without using Text() to avoid warnings
				hasText := false
				for _, candidate := range resp.Candidates {
					if candidate.Content != nil {
						for _, part := range candidate.Content.Parts {
							if part.Text != "" {
								hasText = true
								break
							}
						}
					}
					if hasText {
						break
					}
				}

				// Check for function calls
				hasFuncs := len(resp.FunctionCalls()) > 0

				// Send response if it has content or function calls
				if hasText || hasFuncs {
					if hasText {
						hasContent = true
					}
					if hasFuncs {
						hasToolCalls = true
					}
					adapter.mu.Lock()
					adapter.lastResponse = resp // Store for final message
					adapter.mu.Unlock()
					adapter.ch <- result{resp: resp}
				}
			}

			return true
		})

		// Send final message with appropriate stop reason
		if hasContent || hasToolCalls {
			// Use the last response if available to preserve function calls
			adapter.mu.Lock()
			finalResp := adapter.lastResponse
			adapter.mu.Unlock()
			if finalResp == nil {
				finalResp = &genai.GenerateContentResponse{}
			}
			adapter.ch <- result{done: true, resp: finalResp}
		}
	}()

	return adapter
}

// Recv gets the next Gemini content chunk
func (g *StreamAdapter) Recv() (chat.MessageStreamResponse, error) {
	res, ok := <-g.ch
	if !ok {
		return chat.MessageStreamResponse{}, io.EOF
	}

	if res.err != nil {
		return chat.MessageStreamResponse{}, res.err
	}

	// Build response
	resp := chat.MessageStreamResponse{
		Model:   g.model,
		Choices: []chat.MessageStreamChoice{{}},
	}

	if res.done {
		// Set finish reason and role
		resp.Choices[0].Delta.Role = string(chat.MessageRoleAssistant)

		// Check if we have function calls in the final response
		if res.resp != nil && len(res.resp.FunctionCalls()) > 0 {
			resp.Choices[0].FinishReason = chat.FinishReasonToolCalls
			// Don't include function calls in the final message - they were already sent
			slog.Debug("Gemini: Final message with tool calls finish reason")
		} else {
			resp.Choices[0].FinishReason = chat.FinishReasonStop
		}
	} else if res.resp != nil {
		resp.ID = res.resp.ResponseID

		// Handle token usage if present
		if res.resp.UsageMetadata != nil {
			resp.Usage = &chat.Usage{
				InputTokens:        int(res.resp.UsageMetadata.PromptTokenCount),
				OutputTokens:       int(res.resp.UsageMetadata.CandidatesTokenCount),
				CachedInputTokens:  int(res.resp.UsageMetadata.CachedContentTokenCount),
				CachedOutputTokens: 0, // Gemini doesn't provide cached output tokens
				ReasoningTokens:    int(res.resp.UsageMetadata.ThoughtsTokenCount),
			}
		}

		// Handle text and thoughts separately so TUI can render them distinctly
		var textContent string
		var reasoningText string
		for _, candidate := range res.resp.Candidates {
			if candidate.Content != nil {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						if part.Thought {
							reasoningText += part.Text
						} else {
							textContent += part.Text
						}
					}
				}
			}
		}
		if reasoningText != "" {
			resp.Choices[0].Delta.ReasoningContent = reasoningText
		}
		if textContent != "" {
			resp.Choices[0].Delta.Content = textContent
		}

		// Handle function calls
		if funcs := res.resp.FunctionCalls(); len(funcs) > 0 {
			resp.Choices[0].Delta.ToolCalls = []tools.ToolCall{}
			for _, fc := range funcs {
				argsJSON, _ := json.Marshal(fc.Args)
				id := "call_" + uuid.New().String()
				slog.Debug("Gemini: Function call", "name", fc.Name, "args", string(argsJSON), "id", id)
				resp.Choices[0].Delta.ToolCalls = append(resp.Choices[0].Delta.ToolCalls, tools.ToolCall{
					ID:   id,
					Type: "function",
					Function: tools.FunctionCall{
						Name:      fc.Name,
						Arguments: string(argsJSON),
					},
				})
				slog.Debug("Gemini: Sending tool call", "name", fc.Name, "args", string(argsJSON), "id", id)
			}
		}
	}

	return resp, nil
}

// Close closes the stream
func (g *StreamAdapter) Close() {
	// Drain channel to let goroutine exit
	go func() {
		for range g.ch {
		}
	}()
}

// extractTextFromError attempts to extract text content from streaming parsing errors
func extractTextFromError(errMsg string) string {
	// Look for the JSON response in the error message
	// The error typically contains something like: invalid stream chunk: [{...JSON...}]

	// First try to find the complete JSON object
	startIdx := strings.Index(errMsg, "[{")
	if startIdx == -1 {
		return ""
	}

	// Find the matching closing bracket
	jsonStart := startIdx + 1 // Skip the opening [
	bracketCount := 0
	var jsonEnd int

	for i := jsonStart; i < len(errMsg); i++ {
		char := errMsg[i]
		if char == '{' {
			bracketCount++
		} else if char == '}' {
			bracketCount--
			if bracketCount == 0 {
				jsonEnd = i + 1
				break
			}
		}
	}

	if bracketCount != 0 || jsonEnd == 0 {
		// Fallback to regex approach for partial text
		textRegex := regexp.MustCompile(`"text"\s*:\s*"([^"\\]*(\\.[^"\\]*)*)"`)
		matches := textRegex.FindStringSubmatch(errMsg)

		if len(matches) > 1 {
			textContent := matches[1]
			textContent = strings.ReplaceAll(textContent, `\"`, `"`)
			textContent = strings.ReplaceAll(textContent, `\\`, `\`)
			textContent = strings.ReplaceAll(textContent, `\n`, "\n")
			textContent = strings.ReplaceAll(textContent, `\t`, "\t")
			return textContent
		}
		return ""
	}

	// Extract the JSON string
	jsonStr := errMsg[jsonStart:jsonEnd]

	// Try to parse the JSON to extract text
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &response); err == nil {
		if len(response.Candidates) > 0 && len(response.Candidates[0].Content.Parts) > 0 {
			return response.Candidates[0].Content.Parts[0].Text
		}
	}

	// Final fallback to regex approach
	textRegex := regexp.MustCompile(`"text"\s*:\s*"([^"\\]*(\\.[^"\\]*)*)"`)
	matches := textRegex.FindStringSubmatch(errMsg)

	if len(matches) > 1 {
		textContent := matches[1]
		textContent = strings.ReplaceAll(textContent, `\"`, `"`)
		textContent = strings.ReplaceAll(textContent, `\\`, `\`)
		textContent = strings.ReplaceAll(textContent, `\n`, "\n")
		textContent = strings.ReplaceAll(textContent, `\t`, "\t")
		return textContent
	}

	return ""
}
