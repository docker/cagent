package runtime

import (
	"context"
	_ "embed"
	"log/slog"
	"time"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

//go:embed prompts/compaction-system.txt
var compactionSystemPrompt string

//go:embed prompts/compaction-user.txt
var compactionUserPrompt string

type sessionCompactor struct {
	model        provider.Provider
	sessionStore session.Store
}

func newSessionCompactor(model provider.Provider, sessionStore session.Store) *sessionCompactor {
	return &sessionCompactor{
		model:        model,
		sessionStore: sessionStore,
	}
}

func (c *sessionCompactor) Compact(ctx context.Context, sess *session.Session, additionalPrompt string, events chan Event, agentName string) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", agentName)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", agentName)
	}()

	summaryModel := provider.CloneWithOptions(ctx, c.model, options.WithStructuredOutput(nil))
	root := agent.New("root", compactionSystemPrompt, agent.WithModel(summaryModel))
	newTeam := team.New(team.WithAgents(root))

	messages := sess.GetMessages(root)
	if !hasConversationMessages(messages) {
		events <- Warning("Session is empty. Start a conversation before compacting.", agentName)
		return
	}

	summarySession := session.New()
	summarySession.Title = "Generating summary..."
	for _, msg := range messages {
		summarySession.AddMessage(&session.Message{Message: msg})
	}

	prompt := compactionUserPrompt
	if additionalPrompt != "" {
		prompt += "\n\nAdditional instructions from user: " + additionalPrompt
	}
	summarySession.AddMessage(&session.Message{
		Message: chat.Message{
			Role:      chat.MessageRoleUser,
			Content:   prompt,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	})

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create summary generator runtime", "error", err)
		events <- Error(err.Error())
		return
	}

	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		slog.Error("Failed to generate session summary", "error", err)
		events <- Error(err.Error())
		return
	}

	summary := summarySession.GetLastAssistantMessageContent()
	if summary == "" {
		return
	}

	sess.Messages = append(sess.Messages, session.Item{Summary: summary})
	_ = c.sessionStore.UpdateSession(ctx, sess)

	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, agentName)
}

func hasConversationMessages(messages []chat.Message) bool {
	for _, msg := range messages {
		if msg.Role != chat.MessageRoleSystem {
			return true
		}
	}
	return false
}
