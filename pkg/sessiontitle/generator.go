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

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
)

const (
	systemPrompt     = "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given up to 2 recent user messages and asked to create a single-line title that captures the main topic. Never use newlines or line breaks in your response."
	userPromptFormat = "Based on the following recent user messages from a conversation with an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text on a single line, nothing else. Do not include any newlines, explanations, or formatting.\n\nRecent user messages:\n%s\n\n"
)

// Generator generates session titles using a one-shot LLM completion.
type Generator struct {
	model provider.Provider
}

// New creates a new title Generator with the given model provider.
func New(model provider.Provider) *Generator {
	return &Generator{
		model: model,
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

	slog.Debug("Generating title for session", "session_id", sessionID, "message_count", len(userMessages))

	// Format messages for the prompt
	var formattedMessages strings.Builder
	for i, msg := range userMessages {
		fmt.Fprintf(&formattedMessages, "%d. %s\n", i+1, msg)
	}
	userPrompt := fmt.Sprintf(userPromptFormat, formattedMessages.String())

	// Clone the model with title-generation-specific options
	titleModel := provider.CloneWithOptions(
		ctx,
		g.model,
		options.WithStructuredOutput(nil),
		options.WithMaxTokens(20),
		options.WithGeneratingTitle(),
		options.WithThinking(false), // Disable thinking to avoid max_tokens < thinking_budget errors
	)

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

	// Call the provider directly (no tools needed for title generation)
	stream, err := titleModel.CreateChatCompletionStream(ctx, messages, nil)
	if err != nil {
		slog.Error("Failed to create title generation stream", "session_id", sessionID, "error", err)
		return "", fmt.Errorf("creating title stream: %w", err)
	}
	defer stream.Close()

	// Drain the stream to collect the full title
	var title strings.Builder
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slog.Error("Error receiving from title stream", "session_id", sessionID, "error", err)
			return "", fmt.Errorf("receiving from title stream: %w", err)
		}

		if len(response.Choices) > 0 {
			title.WriteString(response.Choices[0].Delta.Content)
		}
	}

	result := sanitizeTitle(title.String())
	if result == "" {
		return "", nil
	}

	slog.Debug("Generated session title", "session_id", sessionID, "title", result)
	return result, nil
}

// sanitizeTitle ensures the title is a single line by taking only the first
// non-empty line and stripping any control characters that could break TUI rendering.
func sanitizeTitle(title string) string {
	// Split by newlines and take the first non-empty line
	lines := strings.Split(title, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Remove any remaining carriage returns
			line = strings.ReplaceAll(line, "\r", "")
			return line
		}
	}
	return ""
}
