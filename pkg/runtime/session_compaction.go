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
		// Copy messages without their cost — the summary session should
		// only track the cost of generating the summary itself, not the
		// original conversation costs (which are already accounted for
		// in the parent session).
		cloned := msg
		cloned.Cost = 0
		summarySession.AddMessage(&session.Message{Message: cloned})
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

	compactionCost := summarySession.TotalCost()

	// Store the compaction cost on the summary item so that TotalCost()
	// can discover it when walking the session tree.
	sess.Messages = append(sess.Messages, session.Item{Summary: summary, Cost: compactionCost})

	// Update the parent session's token counts to reflect the compacted
	// context. The summary model's output tokens approximate the new
	// context size (system prompt + summary). The old counts reflected
	// the pre-compaction context and are no longer meaningful.
	sess.InputTokens = summarySession.OutputTokens
	sess.OutputTokens = 0

	_ = c.sessionStore.UpdateSession(ctx, sess)

	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary), "compaction_cost", compactionCost)
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
