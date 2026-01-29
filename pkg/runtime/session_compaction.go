package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const (
	compactionSystemPrompt = "You are a helpful AI assistant that creates comprehensive summaries of conversations. You will be given a conversation history and asked to create a concise yet thorough summary that captures the key points, decisions made, and outcomes."
	compactionUserPrompt   = `Based on the following conversation between a user and an AI assistant, create a comprehensive summary that captures:
- The main topics discussed
- Key information exchanged
- Decisions made or conclusions reached
- Important outcomes or results

Provide a well-structured summary (2-4 paragraphs) that someone could read to understand what happened in this conversation. Return ONLY the summary text, nothing else.

Conversation history:%s

Generate a summary for this conversation:`
)

type sessionCompactor struct {
	model provider.Provider
}

func newSessionCompactor(model provider.Provider) *sessionCompactor {
	return &sessionCompactor{
		model: model,
	}
}

func (c *sessionCompactor) Compact(ctx context.Context, sess *session.Session, additionalPrompt string, events chan Event, agentName string) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", agentName)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", agentName)
	}()

	messages := sess.GetAllMessages()
	if len(messages) == 0 {
		events <- Warning("Session is empty. Start a conversation before compacting.", agentName)
		return
	}

	conversationHistory := c.buildConversationHistory(messages)
	userPrompt := c.buildUserPrompt(conversationHistory, additionalPrompt)

	summary := c.generateSummary(ctx, userPrompt)
	if summary == "" {
		return
	}

	sess.Messages = append(sess.Messages, session.Item{Summary: summary})

	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, agentName)
}

func (c *sessionCompactor) buildConversationHistory(messages []session.Message) string {
	var builder strings.Builder
	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			continue
		}
		fmt.Fprintf(&builder, "\n%s: %s", role, messages[i].Message.Content)
	}
	return builder.String()
}

func (c *sessionCompactor) buildUserPrompt(conversationHistory, additionalPrompt string) string {
	prompt := fmt.Sprintf(compactionUserPrompt, conversationHistory)
	if additionalPrompt != "" {
		prompt += fmt.Sprintf("\n\nAdditional instructions from user: %s", additionalPrompt)
	}
	return prompt
}

func (c *sessionCompactor) generateSummary(ctx context.Context, userPrompt string) string {
	summaryModel := provider.CloneWithOptions(ctx, c.model, options.WithStructuredOutput(nil))
	newTeam := team.New(
		team.WithAgents(agent.New("root", compactionSystemPrompt, agent.WithModel(summaryModel))),
	)

	summarySession := session.New(session.WithSystemMessage(compactionSystemPrompt))
	summarySession.AddMessage(session.UserMessage(userPrompt))
	summarySession.Title = "Generating summary..."

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create summary generator runtime", "error", err)
		return ""
	}

	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		slog.Error("Failed to generate session summary", "error", err)
		return ""
	}

	return summarySession.GetLastAssistantMessageContent()
}
