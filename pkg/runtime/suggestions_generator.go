package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const (
	suggestionsSystemPrompt = `You are a helpful AI assistant. Based on the conversation, suggest follow-up actions or questions the user might want to explore next.

Rules:
- Keep each suggestion very concise (under 8 words)
- Use action verbs or short questions
- No explanations, just the suggestion text

Return suggestions as a JSON array of strings:
["suggestion 1", "suggestion 2", "suggestion 3"]

Return ONLY the JSON array, nothing else. Order suggestions by relevance (most relevant first).`

	suggestionsUserPromptFormat = `Based on this conversation, suggest %d follow-up options:

Last user message: %s

Last assistant response: %s

Provide exactly %d suggestions as a JSON array.`

	defaultMaxCount   = 3
	defaultTimeoutMs  = 5000
	maxResponseTokens = 300
)

type suggestionsGenerator struct {
	wg      sync.WaitGroup
	model   provider.Provider
	config  *latest.FollowUpSuggestionsConfig
	enabled bool
}

func newSuggestionsGenerator(model provider.Provider, config *latest.FollowUpSuggestionsConfig) *suggestionsGenerator {
	enabled := config != nil && config.Enabled
	return &suggestionsGenerator{
		model:   model,
		config:  config,
		enabled: enabled,
	}
}

func (s *suggestionsGenerator) Generate(ctx context.Context, sess *session.Session, events chan<- Event, agentName string) {
	if !s.enabled {
		return
	}

	s.wg.Go(func() {
		s.generate(ctx, sess, events, agentName)
	})
}

func (s *suggestionsGenerator) Wait() {
	s.wg.Wait()
}

func (s *suggestionsGenerator) generate(ctx context.Context, sess *session.Session, events chan<- Event, agentName string) {
	// Apply timeout
	timeout := time.Duration(defaultTimeoutMs) * time.Millisecond
	if s.config != nil && s.config.TimeoutMs > 0 {
		timeout = time.Duration(s.config.TimeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slog.Debug("Generating follow-up suggestions", "session_id", sess.ID)

	lastUserMsg := sess.GetLastUserMessageContent()
	lastAssistantMsg := sess.GetLastAssistantMessageContent()

	if lastUserMsg == "" || lastAssistantMsg == "" {
		slog.Debug("Skipping suggestions generation: missing user or assistant message")
		return
	}

	maxCount := defaultMaxCount
	if s.config != nil && s.config.MaxCount > 0 {
		maxCount = s.config.MaxCount
	}

	userPrompt := fmt.Sprintf(suggestionsUserPromptFormat, maxCount, lastUserMsg, lastAssistantMsg, maxCount)

	suggestionsModel := provider.CloneWithOptions(
		ctx,
		s.model,
		options.WithStructuredOutput(nil),
		options.WithMaxTokens(maxResponseTokens),
	)

	newTeam := team.New(
		team.WithAgents(agent.New("root", suggestionsSystemPrompt, agent.WithModel(suggestionsModel))),
	)

	suggestionsSession := session.New(
		session.WithUserMessage(userPrompt),
	)

	suggestionsRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create suggestions generator runtime", "error", err)
		return
	}

	_, err = suggestionsRuntime.Run(ctx, suggestionsSession)
	if err != nil {
		slog.Error("Failed to generate suggestions", "session_id", sess.ID, "error", err)
		return
	}

	response := suggestionsSession.GetLastAssistantMessageContent()
	suggestions := parseSuggestions(response, maxCount)

	if len(suggestions) > 0 {
		events <- FollowUpSuggestions(suggestions, agentName)
	}
}

func parseSuggestions(response string, maxCount int) []string {
	var suggestions []string

	// Clean up response - models often wrap JSON in markdown code blocks
	cleanResponse := response
	if idx := strings.Index(response, "["); idx != -1 {
		if endIdx := strings.LastIndex(response, "]"); endIdx > idx {
			cleanResponse = response[idx : endIdx+1]
		}
	}

	if err := json.Unmarshal([]byte(cleanResponse), &suggestions); err != nil {
		return nil
	}

	// Validate and limit suggestions
	var valid []string
	for _, s := range suggestions {
		if s == "" {
			continue
		}
		valid = append(valid, s)
		if len(valid) >= maxCount {
			break
		}
	}

	return valid
}
