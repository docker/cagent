// Package connectrpc provides a Connect-RPC server implementation for the cagent API.
package connectrpc

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	cagentv1 "github.com/docker/cagent/gen/proto/cagent/v1"
	"github.com/docker/cagent/gen/proto/cagent/v1/cagentv1connect"
	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// Server implements the Connect-RPC AgentService.
type Server struct {
	sm *server.SessionManager
}

// New creates a new Connect-RPC server.
func New(ctx context.Context, sessionStore session.Store, runConfig *config.RuntimeConfig, refreshInterval time.Duration, agentSources config.Sources) (*Server, error) {
	return &Server{
		sm: server.NewSessionManager(ctx, agentSources, sessionStore, refreshInterval, runConfig),
	}, nil
}

// Handler returns an http.Handler for the Connect-RPC server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	path, handler := cagentv1connect.NewAgentServiceHandler(s)
	mux.Handle(path, handler)
	return h2c.NewHandler(mux, &http2.Server{})
}

// Serve starts the Connect-RPC server on the given listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		slog.Error("Failed to start Connect-RPC server", "error", err)
		return err
	}

	return nil
}

// ListAgents returns all available agents.
func (s *Server) ListAgents(ctx context.Context, _ *connect.Request[cagentv1.ListAgentsRequest]) (*connect.Response[cagentv1.ListAgentsResponse], error) {
	agents := []*cagentv1.Agent{}
	for k, agentSource := range s.sm.Sources {
		slog.Debug("API source", "source", agentSource.Name())

		c, err := config.Load(ctx, agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		desc := c.Agents.First().Description

		switch {
		case len(c.Agents) > 1:
			agents = append(agents, &cagentv1.Agent{
				Name:        k,
				Multi:       true,
				Description: desc,
			})
		case len(c.Agents) == 1:
			agents = append(agents, &cagentv1.Agent{
				Name:        k,
				Multi:       false,
				Description: desc,
			})
		default:
			slog.Warn("No agents found in config from API source", "key", k)
			continue
		}
	}

	slices.SortFunc(agents, func(a, b *cagentv1.Agent) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return connect.NewResponse(&cagentv1.ListAgentsResponse{
		Agents: agents,
	}), nil
}

// GetAgent returns the configuration for a specific agent.
func (s *Server) GetAgent(ctx context.Context, req *connect.Request[cagentv1.GetAgentRequest]) (*connect.Response[cagentv1.GetAgentResponse], error) {
	agentID := req.Msg.Id

	for k, agentSource := range s.sm.Sources {
		if k != agentID {
			continue
		}

		slog.Debug("API source", "source", agentSource.Name())
		cfg, err := config.Load(ctx, agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		configJSON, err := json.Marshal(cfg)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal config: %w", err))
		}

		return connect.NewResponse(&cagentv1.GetAgentResponse{
			ConfigJson: configJSON,
		}), nil
	}

	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent not found: %s", agentID))
}

// ListSessions returns all sessions.
func (s *Server) ListSessions(ctx context.Context, _ *connect.Request[cagentv1.ListSessionsRequest]) (*connect.Response[cagentv1.ListSessionsResponse], error) {
	sessions, err := s.sm.GetSessions(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get sessions: %w", err))
	}

	responses := make([]*cagentv1.SessionSummary, len(sessions))
	for i, sess := range sessions {
		responses[i] = &cagentv1.SessionSummary{
			Id:           sess.ID,
			Title:        sess.Title,
			CreatedAt:    sess.CreatedAt.Format(time.RFC3339),
			NumMessages:  int32(len(sess.GetAllMessages())),
			InputTokens:  sess.InputTokens,
			OutputTokens: sess.OutputTokens,
			WorkingDir:   sess.WorkingDir,
		}
	}
	return connect.NewResponse(&cagentv1.ListSessionsResponse{
		Sessions: responses,
	}), nil
}

// GetSession returns a specific session by ID.
func (s *Server) GetSession(ctx context.Context, req *connect.Request[cagentv1.GetSessionRequest]) (*connect.Response[cagentv1.GetSessionResponse], error) {
	sess, err := s.sm.GetSession(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session not found: %w", err))
	}

	messages := sess.GetAllMessages()
	protoMessages := make([]*cagentv1.Message, len(messages))
	for i, msg := range messages {
		protoMessages[i] = sessionMessageToProto(msg)
	}

	return connect.NewResponse(&cagentv1.GetSessionResponse{
		Session: &cagentv1.Session{
			Id:            sess.ID,
			Title:         sess.Title,
			CreatedAt:     sess.CreatedAt.Format(time.RFC3339),
			Messages:      protoMessages,
			ToolsApproved: sess.ToolsApproved,
			InputTokens:   sess.InputTokens,
			OutputTokens:  sess.OutputTokens,
			WorkingDir:    sess.WorkingDir,
		},
	}), nil
}

// CreateSession creates a new session.
func (s *Server) CreateSession(ctx context.Context, req *connect.Request[cagentv1.CreateSessionRequest]) (*connect.Response[cagentv1.CreateSessionResponse], error) {
	sessionTemplate := &session.Session{
		MaxIterations: int(req.Msg.MaxIterations),
		ToolsApproved: req.Msg.ToolsApproved,
		WorkingDir:    req.Msg.WorkingDir,
		// Note: Permissions are not yet supported in proto - would need proto schema update
	}

	sess, err := s.sm.CreateSession(ctx, sessionTemplate)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create session: %w", err))
	}

	return connect.NewResponse(&cagentv1.CreateSessionResponse{
		Session: &cagentv1.Session{
			Id:            sess.ID,
			Title:         sess.Title,
			CreatedAt:     sess.CreatedAt.Format(time.RFC3339),
			ToolsApproved: sess.ToolsApproved,
			InputTokens:   sess.InputTokens,
			OutputTokens:  sess.OutputTokens,
			WorkingDir:    sess.WorkingDir,
		},
	}), nil
}

// DeleteSession deletes a session by ID.
func (s *Server) DeleteSession(ctx context.Context, req *connect.Request[cagentv1.DeleteSessionRequest]) (*connect.Response[cagentv1.DeleteSessionResponse], error) {
	if err := s.sm.DeleteSession(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete session: %w", err))
	}
	return connect.NewResponse(&cagentv1.DeleteSessionResponse{}), nil
}

// ResumeSession resumes a paused session.
func (s *Server) ResumeSession(ctx context.Context, req *connect.Request[cagentv1.ResumeSessionRequest]) (*connect.Response[cagentv1.ResumeSessionResponse], error) {
	if err := s.sm.ResumeSession(ctx, req.Msg.Id, req.Msg.Confirmation, req.Msg.Reason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume session: %w", err))
	}
	return connect.NewResponse(&cagentv1.ResumeSessionResponse{}), nil
}

// ToggleToolApproval toggles the YOLO mode for a session.
func (s *Server) ToggleToolApproval(ctx context.Context, req *connect.Request[cagentv1.ToggleToolApprovalRequest]) (*connect.Response[cagentv1.ToggleToolApprovalResponse], error) {
	if err := s.sm.ToggleToolApproval(ctx, req.Msg.SessionId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to toggle tool approval: %w", err))
	}
	return connect.NewResponse(&cagentv1.ToggleToolApprovalResponse{}), nil
}

// UpdateSessionTitle updates the title of a session.
func (s *Server) UpdateSessionTitle(ctx context.Context, req *connect.Request[cagentv1.UpdateSessionTitleRequest]) (*connect.Response[cagentv1.UpdateSessionTitleResponse], error) {
	if err := s.sm.UpdateSessionTitle(ctx, req.Msg.SessionId, req.Msg.Title); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update session title: %w", err))
	}
	return connect.NewResponse(&cagentv1.UpdateSessionTitleResponse{
		SessionId: req.Msg.SessionId,
		Title:     req.Msg.Title,
	}), nil
}

// ResumeElicitation resumes an elicitation request.
func (s *Server) ResumeElicitation(ctx context.Context, req *connect.Request[cagentv1.ResumeElicitationRequest]) (*connect.Response[cagentv1.ResumeElicitationResponse], error) {
	var content map[string]any
	if len(req.Msg.ContentJson) > 0 {
		if err := json.Unmarshal(req.Msg.ContentJson, &content); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid content JSON: %w", err))
		}
	}

	if err := s.sm.ResumeElicitation(ctx, req.Msg.SessionId, req.Msg.Action, content); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume elicitation: %w", err))
	}
	return connect.NewResponse(&cagentv1.ResumeElicitationResponse{}), nil
}

// RunAgent runs an agent loop and streams events.
func (s *Server) RunAgent(ctx context.Context, req *connect.Request[cagentv1.RunAgentRequest], stream *connect.ServerStream[cagentv1.Event]) error {
	sessionID := req.Msg.SessionId
	agentFilename := req.Msg.Agent
	currentAgent := cmp.Or(req.Msg.AgentName, "root")

	slog.Debug("Running agent via Connect-RPC", "agent_filename", agentFilename, "session_id", sessionID, "current_agent", currentAgent)

	// Convert input messages
	messages := make([]api.Message, len(req.Msg.Messages))
	for i, msg := range req.Msg.Messages {
		messages[i] = api.Message{
			Content: msg.Content,
		}
	}

	streamChan, err := s.sm.RunSession(ctx, sessionID, agentFilename, currentAgent, messages)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to run session: %w", err))
	}

	for event := range streamChan {
		protoEvent := runtimeEventToProto(event)
		if protoEvent == nil {
			continue
		}
		if err := stream.Send(protoEvent); err != nil {
			return err
		}
	}

	return nil
}

// Ping is a health check endpoint.
func (s *Server) Ping(_ context.Context, _ *connect.Request[cagentv1.PingRequest]) (*connect.Response[cagentv1.PingResponse], error) {
	return connect.NewResponse(&cagentv1.PingResponse{
		Status: "ok",
	}), nil
}

// Helper functions for converting between types

func sessionMessageToProto(msg session.Message) *cagentv1.Message {
	protoMsg := &cagentv1.Message{
		Role:             string(msg.Message.Role),
		Content:          msg.Message.Content,
		CreatedAt:        msg.Message.CreatedAt,
		ToolCallId:       msg.Message.ToolCallID,
		ReasoningContent: msg.Message.ReasoningContent,
		AgentName:        msg.AgentName,
	}

	if len(msg.Message.ToolCalls) > 0 {
		protoMsg.ToolCalls = make([]*cagentv1.ToolCall, len(msg.Message.ToolCalls))
		for i, tc := range msg.Message.ToolCalls {
			protoMsg.ToolCalls[i] = toolCallToProto(tc)
		}
	}

	return protoMsg
}

func toolCallToProto(tc tools.ToolCall) *cagentv1.ToolCall {
	return &cagentv1.ToolCall{
		Id:   tc.ID,
		Type: string(tc.Type),
		Function: &cagentv1.FunctionCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		},
	}
}

func toolToProto(t tools.Tool) *cagentv1.Tool {
	var paramsJSON []byte
	if t.Parameters != nil {
		paramsJSON, _ = json.Marshal(t.Parameters)
	}

	var outputSchemaJSON []byte
	if t.OutputSchema != nil {
		outputSchemaJSON, _ = json.Marshal(t.OutputSchema)
	}

	annotations := &cagentv1.ToolAnnotations{
		ReadOnlyHint:   t.Annotations.ReadOnlyHint,
		IdempotentHint: t.Annotations.IdempotentHint,
	}
	if t.Annotations.DestructiveHint != nil {
		annotations.DestructiveHint = *t.Annotations.DestructiveHint
	}
	if t.Annotations.OpenWorldHint != nil {
		annotations.OpenWorldHint = *t.Annotations.OpenWorldHint
	}

	return &cagentv1.Tool{
		Name:             t.Name,
		Category:         t.Category,
		Description:      t.Description,
		ParametersJson:   paramsJSON,
		Annotations:      annotations,
		OutputSchemaJson: outputSchemaJSON,
	}
}

func toolCallResultToProto(r *tools.ToolCallResult) *cagentv1.ToolCallResult {
	if r == nil {
		return nil
	}

	var metaJSON []byte
	if r.Meta != nil {
		metaJSON, _ = json.Marshal(r.Meta)
	}

	return &cagentv1.ToolCallResult{
		Output:   r.Output,
		IsError:  r.IsError,
		MetaJson: metaJSON,
	}
}

func messageUsageToProto(m *runtime.MessageUsage) *cagentv1.LastMessageUsage {
	if m == nil {
		return nil
	}
	return &cagentv1.LastMessageUsage{
		InputTokens:       m.InputTokens,
		OutputTokens:      m.OutputTokens,
		CachedInputTokens: m.CachedInputTokens,
		CacheWriteTokens:  m.CacheWriteTokens,
		Cost:              m.Cost,
		Model:             m.Model,
	}
}

func runtimeEventToProto(event runtime.Event) *cagentv1.Event {
	switch e := event.(type) {
	case *runtime.UserMessageEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_UserMessage{
				UserMessage: &cagentv1.UserMessageEvent{
					Message: e.Message,
				},
			},
		}

	case *runtime.StreamStartedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_StreamStarted{
				StreamStarted: &cagentv1.StreamStartedEvent{
					SessionId: e.SessionID,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.StreamStoppedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_StreamStopped{
				StreamStopped: &cagentv1.StreamStoppedEvent{
					SessionId: e.SessionID,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.AgentChoiceEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_AgentChoice{
				AgentChoice: &cagentv1.AgentChoiceEvent{
					Content:   e.Content,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.AgentChoiceReasoningEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_AgentChoiceReasoning{
				AgentChoiceReasoning: &cagentv1.AgentChoiceReasoningEvent{
					Content:   e.Content,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.PartialToolCallEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_PartialToolCall{
				PartialToolCall: &cagentv1.PartialToolCallEvent{
					ToolCall:       toolCallToProto(e.ToolCall),
					ToolDefinition: toolToProto(e.ToolDefinition),
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.ToolCallEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_ToolCall{
				ToolCall: &cagentv1.ToolCallEvent{
					ToolCall:       toolCallToProto(e.ToolCall),
					ToolDefinition: toolToProto(e.ToolDefinition),
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.ToolCallConfirmationEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_ToolCallConfirmation{
				ToolCallConfirmation: &cagentv1.ToolCallConfirmationEvent{
					ToolCall:       toolCallToProto(e.ToolCall),
					ToolDefinition: toolToProto(e.ToolDefinition),
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.ToolCallResponseEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_ToolCallResponse{
				ToolCallResponse: &cagentv1.ToolCallResponseEvent{
					ToolCall:       toolCallToProto(e.ToolCall),
					ToolDefinition: toolToProto(e.ToolDefinition),
					Response:       e.Response,
					Result:         toolCallResultToProto(e.Result),
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.ErrorEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_Error{
				Error: &cagentv1.ErrorEvent{
					Error:     e.Error,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.WarningEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_Warning{
				Warning: &cagentv1.WarningEvent{
					Message:   e.Message,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.TokenUsageEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_TokenUsage{
				TokenUsage: &cagentv1.TokenUsageEvent{
					SessionId: e.SessionID,
					Usage: &cagentv1.Usage{
						InputTokens:   e.Usage.InputTokens,
						OutputTokens:  e.Usage.OutputTokens,
						ContextLength: e.Usage.ContextLength,
						ContextLimit:  e.Usage.ContextLimit,
						Cost:          e.Usage.Cost,
						LastMessage:   messageUsageToProto(e.Usage.LastMessage),
					},
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.SessionTitleEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_SessionTitle{
				SessionTitle: &cagentv1.SessionTitleEvent{
					SessionId: e.SessionID,
					Title:     e.Title,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.SessionSummaryEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_SessionSummary{
				SessionSummary: &cagentv1.SessionSummaryEvent{
					SessionId: e.SessionID,
					Summary:   e.Summary,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.SessionCompactionEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_SessionCompaction{
				SessionCompaction: &cagentv1.SessionCompactionEvent{
					SessionId: e.SessionID,
					Status:    e.Status,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.ElicitationRequestEvent:
		var schemaJSON []byte
		if e.Schema != nil {
			schemaJSON, _ = json.Marshal(e.Schema)
		}
		var metaJSON []byte
		if e.Meta != nil {
			metaJSON, _ = json.Marshal(e.Meta)
		}
		return &cagentv1.Event{
			Event: &cagentv1.Event_ElicitationRequest{
				ElicitationRequest: &cagentv1.ElicitationRequestEvent{
					Message:    e.Message,
					SchemaJson: schemaJSON,
					MetaJson:   metaJSON,
					AgentName:  e.AgentName,
				},
			},
		}

	case *runtime.AuthorizationEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_Authorization{
				Authorization: &cagentv1.AuthorizationEvent{
					Confirmation: string(e.Confirmation),
					AgentName:    e.AgentName,
				},
			},
		}

	case *runtime.MaxIterationsReachedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_MaxIterationsReached{
				MaxIterationsReached: &cagentv1.MaxIterationsReachedEvent{
					MaxIterations: int32(e.MaxIterations),
					AgentName:     e.AgentName,
				},
			},
		}

	case *runtime.MCPInitStartedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_McpInitStarted{
				McpInitStarted: &cagentv1.MCPInitStartedEvent{
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.MCPInitFinishedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_McpInitFinished{
				McpInitFinished: &cagentv1.MCPInitFinishedEvent{
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.AgentInfoEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_AgentInfo{
				AgentInfo: &cagentv1.AgentInfoEvent{
					AgentName:      e.AgentName,
					Model:          e.Model,
					Description:    e.Description,
					WelcomeMessage: e.WelcomeMessage,
				},
			},
		}

	case *runtime.TeamInfoEvent:
		agents := make([]*cagentv1.AgentDetails, len(e.AvailableAgents))
		for i, a := range e.AvailableAgents {
			agents[i] = &cagentv1.AgentDetails{
				Name:        a.Name,
				Description: a.Description,
				Provider:    a.Provider,
				Model:       a.Model,
			}
		}
		return &cagentv1.Event{
			Event: &cagentv1.Event_TeamInfo{
				TeamInfo: &cagentv1.TeamInfoEvent{
					AvailableAgents: agents,
					CurrentAgent:    e.CurrentAgent,
					AgentName:       e.AgentName,
				},
			},
		}

	case *runtime.AgentSwitchingEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_AgentSwitching{
				AgentSwitching: &cagentv1.AgentSwitchingEvent{
					Switching: e.Switching,
					FromAgent: e.FromAgent,
					ToAgent:   e.ToAgent,
					AgentName: e.AgentName,
				},
			},
		}

	case *runtime.ToolsetInfoEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_ToolsetInfo{
				ToolsetInfo: &cagentv1.ToolsetInfoEvent{
					AvailableTools: int32(e.AvailableTools),
					Loading:        e.Loading,
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.RAGIndexingStartedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_RagIndexingStarted{
				RagIndexingStarted: &cagentv1.RAGIndexingStartedEvent{
					RagName:      e.RAGName,
					StrategyName: e.StrategyName,
					AgentName:    e.AgentName,
				},
			},
		}

	case *runtime.RAGIndexingProgressEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_RagIndexingProgress{
				RagIndexingProgress: &cagentv1.RAGIndexingProgressEvent{
					RagName:      e.RAGName,
					StrategyName: e.StrategyName,
					Current:      int32(e.Current),
					Total:        int32(e.Total),
					AgentName:    e.AgentName,
				},
			},
		}

	case *runtime.RAGIndexingCompletedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_RagIndexingCompleted{
				RagIndexingCompleted: &cagentv1.RAGIndexingCompletedEvent{
					RagName:      e.RAGName,
					StrategyName: e.StrategyName,
					AgentName:    e.AgentName,
				},
			},
		}

	case *runtime.HookBlockedEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_HookBlocked{
				HookBlocked: &cagentv1.HookBlockedEvent{
					ToolCall:       toolCallToProto(e.ToolCall),
					ToolDefinition: toolToProto(e.ToolDefinition),
					Message:        e.Message,
					AgentName:      e.AgentName,
				},
			},
		}

	case *runtime.ShellOutputEvent:
		return &cagentv1.Event{
			Event: &cagentv1.Event_ShellOutput{
				ShellOutput: &cagentv1.ShellOutputEvent{
					Output: e.Output,
				},
			},
		}

	default:
		slog.Warn("Unknown runtime event type", "type", fmt.Sprintf("%T", event))
		return nil
	}
}
