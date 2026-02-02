package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

// PersistentRuntime wraps a LocalRuntime and persists session changes to a store
// based on emitted events.
type PersistentRuntime struct {
	*LocalRuntime
}

// streamingState tracks the accumulated content for a streaming assistant message
type streamingState struct {
	content          strings.Builder
	reasoningContent strings.Builder
	agentName        string
	messageID        int64 // ID of the current streaming message (0 if none)
}

// New creates a new runtime for an agent and its team.
// The runtime automatically persists session changes to the configured store.
// Returns a Runtime interface which wraps LocalRuntime with persistence handling.
func New(agents *team.Team, opts ...Opt) (Runtime, error) {
	r, err := NewLocalRuntime(agents, opts...)
	if err != nil {
		return nil, err
	}

	return &PersistentRuntime{
		LocalRuntime: r,
	}, nil
}

// RunStream wraps the inner runtime's RunStream and intercepts events
// to persist session changes to the store.
func (r *PersistentRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	if !sess.IsSubSession() {
		if err := r.sessionStore.UpdateSession(ctx, sess); err != nil {
			slog.Warn("Failed to persist initial session", "session_id", sess.ID, "error", err)
		}
	}

	innerEvents := r.LocalRuntime.RunStream(ctx, sess)
	events := make(chan Event, 128)

	go func() {
		defer close(events)

		streaming := &streamingState{}

		for event := range innerEvents {
			r.handleEvent(ctx, sess, event, streaming)
			events <- event
		}
	}()

	return events
}

func (r *PersistentRuntime) handleEvent(ctx context.Context, sess *session.Session, event Event, streaming *streamingState) {
	// Skip persistence for sub-sessions (they're persisted when added to parent)
	if sess.IsSubSession() {
		return
	}

	switch e := event.(type) {
	case *AgentChoiceEvent:
		// Accumulate streaming content
		streaming.content.WriteString(e.Content)
		streaming.agentName = e.AgentName

		r.persistStreamingContent(ctx, sess.ID, streaming)

	case *AgentChoiceReasoningEvent:
		// Accumulate streaming reasoning content
		streaming.reasoningContent.WriteString(e.Content)
		streaming.agentName = e.AgentName

		r.persistStreamingContent(ctx, sess.ID, streaming)

	case *UserMessageEvent:
		// Reset streaming state when a user message is received
		streaming.content.Reset()
		streaming.reasoningContent.Reset()
		streaming.agentName = ""
		streaming.messageID = 0

		if _, err := r.sessionStore.AddMessage(ctx, e.SessionID, session.UserMessage(e.Message)); err != nil {
			slog.Warn("Failed to persist user message", "session_id", e.SessionID, "error", err)
		}

	case *MessageAddedEvent:
		// Finalize the streaming message with complete metadata
		if streaming.messageID != 0 {
			// Update the existing streaming message with final content
			if err := r.sessionStore.UpdateMessage(ctx, streaming.messageID, e.Message); err != nil {
				slog.Warn("Failed to finalize streaming message", "session_id", e.SessionID, "message_id", streaming.messageID, "error", err)
			}
		} else {
			// No streaming message exists, create a new one
			if _, err := r.sessionStore.AddMessage(ctx, e.SessionID, e.Message); err != nil {
				slog.Warn("Failed to persist message", "session_id", e.SessionID, "error", err)
			}
		}

		// Reset streaming state after message is finalized
		streaming.content.Reset()
		streaming.reasoningContent.Reset()
		streaming.agentName = ""
		streaming.messageID = 0

	case *SubSessionCompletedEvent:
		if subSess, ok := e.SubSession.(*session.Session); ok {
			if err := r.sessionStore.AddSubSession(ctx, e.ParentSessionID, subSess); err != nil {
				slog.Warn("Failed to persist sub-session", "parent_id", e.ParentSessionID, "error", err)
			}
		}

	case *SessionSummaryEvent:
		if err := r.sessionStore.AddSummary(ctx, e.SessionID, e.Summary); err != nil {
			slog.Warn("Failed to persist summary", "session_id", e.SessionID, "error", err)
		}

	case *TokenUsageEvent:
		if e.Usage != nil {
			if err := r.sessionStore.UpdateSessionTokens(ctx, sess.ID, e.Usage.InputTokens, e.Usage.OutputTokens, e.Usage.Cost); err != nil {
				slog.Warn("Failed to persist token usage", "session_id", sess.ID, "error", err)
			}
		}

	case *SessionTitleEvent:
		if err := r.sessionStore.UpdateSessionTitle(ctx, sess.ID, e.Title); err != nil {
			slog.Warn("Failed to persist session title", "session_id", sess.ID, "error", err)
		}
	}
}

// persistStreamingContent creates or updates the streaming assistant message
func (r *PersistentRuntime) persistStreamingContent(ctx context.Context, sessionID string, streaming *streamingState) {
	msg := &session.Message{
		AgentName: streaming.agentName,
		Message: chat.Message{
			Role:             chat.MessageRoleAssistant,
			Content:          streaming.content.String(),
			ReasoningContent: streaming.reasoningContent.String(),
		},
	}

	if streaming.messageID == 0 {
		// Create new streaming message
		id, err := r.sessionStore.AddMessage(ctx, sessionID, msg)
		if err != nil {
			slog.Warn("Failed to create streaming message", "session_id", sessionID, "error", err)
			return
		}
		streaming.messageID = id
		slog.Debug("[PERSIST] Created streaming message", "session_id", sessionID, "message_id", id, "agent", streaming.agentName)
	} else {
		// Update existing streaming message
		if err := r.sessionStore.UpdateMessage(ctx, streaming.messageID, msg); err != nil {
			slog.Warn("Failed to update streaming message", "session_id", sessionID, "message_id", streaming.messageID, "error", err)
		}
	}
}

// Run wraps the inner runtime's Run method
func (r *PersistentRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}
