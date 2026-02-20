// Package sessiontitle provides session title generation using a one-shot LLM call.
// It is designed to be independent of pkg/runtime to avoid circular dependencies
// and the overhead of spinning up a nested runtime.
package sessiontitle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
)

const (
	systemPrompt     = "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given up to 2 recent user messages and asked to create a single-line title that captures the main topic. Never use newlines or line breaks in your response."
	userPromptFormat = "Based on the following recent user messages from a conversation with an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text on a single line, nothing else. Do not include any newlines, explanations, or formatting.\n\nRecent user messages:\n%s\n\n"

	// titleGenerationTimeout is the maximum time to wait for title generation.
	// Title generation should be quick since we disable thinking and use low max_tokens.
	// If the API is slow or hanging (e.g., due to server-side thinking), we should timeout.
	titleGenerationTimeout = 30 * time.Second
)

// Generator generates session titles using a one-shot LLM completion.
type Generator struct {
	models []provider.Provider
}

// New creates a new title Generator with the given model provider.
// The first argument is treated as the primary model; any additional models are
// treated as fallbacks (tried in order) if earlier models fail.
func New(model provider.Provider, fallbackModels ...provider.Provider) *Generator {
	// Filter out nil providers to keep Generate simple.
	models := make([]provider.Provider, 0, 1+len(fallbackModels))
	if model != nil {
		models = append(models, model)
	}
	for _, fb := range fallbackModels {
		if fb != nil {
			models = append(models, fb)
		}
	}
	return &Generator{
		models: models,
	}
}

// Generate produces a title for a session based on the provided user messages.
// It performs a one-shot LLM call directly via the provider's CreateChatCompletionStream,
// avoiding the overhead of spinning up a nested runtime.
// Returns an empty string if generation fails or no messages are provided.
func (g *Generator) Generate(ctx context.Context, sessionID string, userMessages []string) (string, error) {
	if len(userMessages) == 0 {
		return "", nil
	}

	// Apply timeout to prevent hanging on slow or unresponsive models
	ctx, cancel := context.WithTimeout(ctx, titleGenerationTimeout)
	defer cancel()
	if g == nil || len(g.models) == 0 {
		return "", nil
	}

	slog.Debug("Generating title for session", "session_id", sessionID, "message_count", len(userMessages))

	// Format messages for the prompt
	var formattedMessages strings.Builder
	for i, msg := range userMessages {
		fmt.Fprintf(&formattedMessages, "%d. %s\n", i+1, msg)
	}
	userPrompt := fmt.Sprintf(userPromptFormat, formattedMessages.String())

	// Build the messages for the completion request
	messages := []chat.Message{
		{
			Role:    chat.MessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    chat.MessageRoleUser,
			Content: userPrompt,
		},
	}

	var lastErr error
	for idx, baseModel := range g.models {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if baseModel == nil {
			continue
		}

		// Clone the model with title-generation-specific options.
		// We do this per-attempt so each model gets a consistent, low-token one-shot call.
		titleModel := provider.CloneWithOptions(
			ctx,
			baseModel,
			options.WithStructuredOutput(nil),
			options.WithMaxTokens(20),
			options.WithGeneratingTitle(),
			options.WithThinking(false), // Disable thinking to avoid max_tokens < thinking_budget errors
		)

		// Call the provider directly (no tools needed for title generation)
		stream, err := titleModel.CreateChatCompletionStream(ctx, messages, nil)
		if err != nil {
			lastErr = err
			slog.Error("Failed to create title generation stream",
				"session_id", sessionID,
				"model", baseModel.ID(),
				"attempt", idx+1,
				"error", err)
			continue
		}

		// Drain the stream to collect the full title
		var title strings.Builder
		var streamErr error
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				streamErr = err
				break
			}
			if len(response.Choices) > 0 {
				title.WriteString(response.Choices[0].Delta.Content)
			}
		}
		stream.Close()

		if streamErr != nil {
			lastErr = streamErr
			slog.Error("Error receiving from title stream",
				"session_id", sessionID,
				"model", baseModel.ID(),
				"attempt", idx+1,
				"error", streamErr)
			continue
		}

		result := sanitizeTitle(title.String())
		if result == "" {
			// Empty/invalid title output - treat as a failure and try fallbacks.
			lastErr = fmt.Errorf("empty title output from model %q", baseModel.ID())
			slog.Debug("Generated empty title, trying next model",
				"session_id", sessionID,
				"model", baseModel.ID(),
				"attempt", idx+1)
			continue
		}

		slog.Debug("Generated session title", "session_id", sessionID, "title", result, "model", baseModel.ID())
		return result, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("generating title failed: %w", lastErr)
	}
	return "", nil
}

// sanitizeTitle ensures the title is a single line by taking only the first
// non-empty line and stripping any control characters that could break TUI rendering.
func sanitizeTitle(title string) string {
	// Split by newlines and take the first non-empty line
	lines := strings.SplitSeq(title, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Remove any remaining carriage returns
			line = strings.ReplaceAll(line, "\r", "")
			return line
		}
	}
	return ""
}
