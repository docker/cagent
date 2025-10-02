package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

// RemoteRuntime implements the Interface using a remote client
type RemoteRuntime struct {
	client        *Client
	currentAgent  string
	agentFilename string
	sessionID     string
	team          *team.Team
}

// RemoteRuntimeOption is a function for configuring the RemoteRuntime
type RemoteRuntimeOption func(*RemoteRuntime)

// WithRemoteCurrentAgent sets the current agent name
func WithRemoteCurrentAgent(agentName string) RemoteRuntimeOption {
	return func(r *RemoteRuntime) {
		r.currentAgent = agentName
	}
}

// WithRemoteAgentFilename sets the agent filename to use with the remote API
func WithRemoteAgentFilename(filename string) RemoteRuntimeOption {
	return func(r *RemoteRuntime) {
		r.agentFilename = filename
	}
}

// NewRemoteRuntime creates a new remote runtime that implements the Interface
func NewRemoteRuntime(client *Client, opts ...RemoteRuntimeOption) (*RemoteRuntime, error) {
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}

	r := &RemoteRuntime{
		client:        client,
		currentAgent:  "root",
		agentFilename: "agent.yaml", // default
		team:          team.New(),   // empty team, will be populated as needed
	}

	for _, opt := range opts {
		opt(r)
	}

	return r, nil
}

// CurrentAgent returns the currently active agent
func (r *RemoteRuntime) CurrentAgent() *agent.Agent {
	// For remote runtime, we create a minimal agent representation
	// In a full implementation, this could fetch agent details from the remote API
	return agent.New(r.currentAgent, fmt.Sprintf("Remote agent: %s", r.currentAgent))
}

// RunStream starts the agent's interaction loop and returns a channel of events
func (r *RemoteRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	slog.Debug("Starting remote runtime stream", "agent", r.currentAgent, "session_id", r.sessionID)
	events := make(chan Event, 128)

	go func() {
		defer close(events)

		// Convert session messages to remote.Message format
		messages := r.convertSessionMessages(sess)

		r.sessionID = sess.ID

		// Start streaming from remote client
		var streamChan <-chan Event
		var err error

		if r.currentAgent != "" && r.currentAgent != "root" {
			streamChan, err = r.client.RunAgentWithAgentName(ctx, r.sessionID, r.agentFilename, r.currentAgent, messages)
		} else {
			streamChan, err = r.client.RunAgent(ctx, r.sessionID, r.agentFilename, messages)
		}

		if err != nil {
			events <- Error(fmt.Sprintf("failed to start remote agent: %v", err))
			return
		}

		for streamEvent := range streamChan {
			events <- streamEvent
		}
	}()

	return events
}

// Run starts the agent's interaction loop and returns the final messages
func (r *RemoteRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}

// Resume allows resuming execution after user confirmation
func (r *RemoteRuntime) Resume(ctx context.Context, confirmationType string) {
	slog.Debug("Resuming remote runtime", "agent", r.currentAgent, "confirmation_type", confirmationType, "session_id", r.sessionID)

	if r.sessionID == "" {
		slog.Error("Cannot resume: no session ID available")
		return
	}

	if err := r.client.ResumeSession(ctx, r.sessionID, confirmationType); err != nil {
		slog.Error("Failed to resume remote session", "error", err, "session_id", r.sessionID)
	}
}

// Summarize generates a summary for the session
func (r *RemoteRuntime) Summarize(ctx context.Context, sess *session.Session, events chan Event) {
	slog.Debug("Summarize not yet implemented for remote runtime", "session_id", r.sessionID)
	// TODO: Implement summarization by either:
	// 1. Adding a summarization endpoint to the remote API
	// 2. Running a summarization agent through the remote client
	events <- SessionSummary(sess.ID, "Summary generation not yet implemented for remote runtime", r.currentAgent)
}

// convertSessionMessages converts session messages to remote API message format
func (r *RemoteRuntime) convertSessionMessages(sess *session.Session) []api.Message {
	sessionMessages := sess.GetAllMessages()
	messages := make([]api.Message, 0, len(sessionMessages))

	for i := range sessionMessages {
		// Only include user and assistant messages for the remote API
		if sessionMessages[i].Message.Role == chat.MessageRoleUser || sessionMessages[i].Message.Role == chat.MessageRoleAssistant {
			messages = append(messages, api.Message{
				Role:    sessionMessages[i].Message.Role,
				Content: sessionMessages[i].Message.Content,
			})
		}
	}

	return messages
}

// Resume allows resuming execution after user confirmation
func (r *RemoteRuntime) ResumeStartAuthorizationFlow(ctx context.Context, confirmationType bool) {
	slog.Debug("Resuming remote runtime", "agent", r.currentAgent, "confirmation_type", confirmationType, "session_id", r.sessionID)

	if r.sessionID == "" {
		slog.Error("Cannot resume: no session ID available")
		return
	}

	if err := r.client.ResumeStartAuthorizationFlow(ctx, r.sessionID, confirmationType); err != nil {
		slog.Error("Failed to resume remote session", "error", err, "session_id", r.sessionID)
	}
}

// Resume allows resuming execution after user confirmation
func (r *RemoteRuntime) ResumeCodeReceived(ctx context.Context, code, state string) error {
	slog.Debug("Resuming remote runtime", "agent", r.currentAgent, "code", code, "state", state, "session_id", r.sessionID)

	if r.sessionID == "" {
		slog.Error("Cannot resume: no session ID available")
		return fmt.Errorf("session ID cannot be empty")
	}

	if err := r.client.ResumeCodeReceived(ctx, code, state); err != nil {
		slog.Error("Failed to resume remote session", "error", err, "session_id", r.sessionID)
		return err
	}

	return nil
}

// Verify that RemoteRuntime implements the Interface
var _ Runtime = (*RemoteRuntime)(nil)
