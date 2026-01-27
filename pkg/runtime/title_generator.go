package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const (
	titleSystemPrompt     = "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given a conversation history and asked to create a single-line title that captures the main topic. Never use newlines or line breaks in your response."
	titleUserPromptFormat = "Based on the following message a user sent to an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text on a single line, nothing else. Do not include any newlines, explanations, or formatting.\n\nUser message: %s\n\n"
)

type titleGenerator struct {
	wg    sync.WaitGroup
	model provider.Provider
}

func newTitleGenerator(model provider.Provider) *titleGenerator {
	return &titleGenerator{
		model: model,
	}
}

func (t *titleGenerator) Generate(ctx context.Context, sess *session.Session, userMessage string, events chan<- Event) {
	if userMessage == "" {
		return
	}
	t.wg.Go(func() {
		t.generate(ctx, sess, userMessage, events)
	})
}

func (t *titleGenerator) Wait() {
	t.wg.Wait()
}

func (t *titleGenerator) generate(ctx context.Context, sess *session.Session, firstUserMessage string, events chan<- Event) {
	slog.Debug("Generating title for session", "session_id", sess.ID)

	userPrompt := fmt.Sprintf(titleUserPromptFormat, firstUserMessage)

	titleModel := provider.CloneWithOptions(
		ctx,
		t.model,
		options.WithStructuredOutput(nil),
		options.WithMaxTokens(20),
		options.WithGeneratingTitle(),
		options.WithThinking(false), // Disable thinking to avoid max_tokens < thinking_budget errors
	)

	newTeam := team.New(
		team.WithAgents(agent.New("root", titleSystemPrompt, agent.WithModel(titleModel))),
	)

	titleSession := session.New(
		session.WithUserMessage(userPrompt),
		session.WithTitle("Generating title…"),
	)

	titleRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create title generator runtime", "error", err)
		return
	}

	_, err = titleRuntime.Run(ctx, titleSession)
	if err != nil {
		slog.Error("Failed to generate session title", "session_id", sess.ID, "error", err)
		return
	}

	title := titleSession.GetLastAssistantMessageContent()
	if title == "" {
		return
	}

	// Sanitize the title to ensure it's a single line
	title = sanitizeTitle(title)
	if title == "" {
		return
	}

	sess.Title = title
	slog.Debug("Generated session title", "session_id", sess.ID, "title", title)
	events <- SessionTitle(sess.ID, title)
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
