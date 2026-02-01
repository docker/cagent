package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	cagentv1 "github.com/docker/cagent/gen/proto/cagent/v1"
	"github.com/docker/cagent/gen/proto/cagent/v1/cagentv1connect"
	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// ConnectRPCClient is a Connect-RPC client for the cagent server API
type ConnectRPCClient struct {
	client cagentv1connect.AgentServiceClient
}

// ConnectRPCClientOption is a function for configuring the ConnectRPCClient
type ConnectRPCClientOption func(*connectRPCClientOptions)

type connectRPCClientOptions struct {
	httpClient *http.Client
}

// WithConnectRPCHTTPClient sets a custom HTTP client
func WithConnectRPCHTTPClient(client *http.Client) ConnectRPCClientOption {
	return func(o *connectRPCClientOptions) {
		o.httpClient = client
	}
}

// NewConnectRPCClient creates a new Connect-RPC client for the cagent server
func NewConnectRPCClient(baseURL string, opts ...ConnectRPCClientOption) (*ConnectRPCClient, error) {
	options := &connectRPCClientOptions{
		httpClient: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
	}

	for _, opt := range opts {
		opt(options)
	}

	client := cagentv1connect.NewAgentServiceClient(
		options.httpClient,
		baseURL,
	)

	return &ConnectRPCClient{
		client: client,
	}, nil
}

// Ping checks if the server is healthy
func (c *ConnectRPCClient) Ping(ctx context.Context) error {
	_, err := c.client.Ping(ctx, connect.NewRequest(&cagentv1.PingRequest{}))
	return err
}

// GetAgents retrieves all available agents
func (c *ConnectRPCClient) GetAgents(ctx context.Context) ([]api.Agent, error) {
	resp, err := c.client.ListAgents(ctx, connect.NewRequest(&cagentv1.ListAgentsRequest{}))
	if err != nil {
		return nil, err
	}

	agents := make([]api.Agent, len(resp.Msg.Agents))
	for i, a := range resp.Msg.Agents {
		agents[i] = api.Agent{
			Name:        a.Name,
			Description: a.Description,
			Multi:       a.Multi,
		}
	}
	return agents, nil
}

// GetAgent retrieves an agent by ID
func (c *ConnectRPCClient) GetAgent(ctx context.Context, id string) (*latest.Config, error) {
	resp, err := c.client.GetAgent(ctx, connect.NewRequest(&cagentv1.GetAgentRequest{Id: id}))
	if err != nil {
		return nil, err
	}

	cfg := &latest.Config{}
	if err := json.Unmarshal(resp.Msg.ConfigJson, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent config: %w", err)
	}

	return cfg, nil
}

// GetSessions retrieves all sessions
func (c *ConnectRPCClient) GetSessions(ctx context.Context) ([]api.SessionsResponse, error) {
	resp, err := c.client.ListSessions(ctx, connect.NewRequest(&cagentv1.ListSessionsRequest{}))
	if err != nil {
		return nil, err
	}

	sessions := make([]api.SessionsResponse, len(resp.Msg.Sessions))
	for i, s := range resp.Msg.Sessions {
		sessions[i] = api.SessionsResponse{
			ID:    s.Id,
			Title: s.Title,
		}
	}
	return sessions, nil
}

// GetSession retrieves a session by ID
func (c *ConnectRPCClient) GetSession(ctx context.Context, id string) (*api.SessionResponse, error) {
	resp, err := c.client.GetSession(ctx, connect.NewRequest(&cagentv1.GetSessionRequest{Id: id}))
	if err != nil {
		return nil, err
	}

	return &api.SessionResponse{
		ID:    resp.Msg.Session.Id,
		Title: resp.Msg.Session.Title,
	}, nil
}

// CreateSession creates a new session
func (c *ConnectRPCClient) CreateSession(ctx context.Context, sessTemplate *session.Session) (*session.Session, error) {
	req := &cagentv1.CreateSessionRequest{
		ToolsApproved: sessTemplate.ToolsApproved,
	}

	resp, err := c.client.CreateSession(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}

	sess := session.New(
		session.WithToolsApproved(resp.Msg.Session.ToolsApproved),
	)
	sess.ID = resp.Msg.Session.Id

	return sess, nil
}

// DeleteSession deletes a session by ID
func (c *ConnectRPCClient) DeleteSession(ctx context.Context, id string) error {
	_, err := c.client.DeleteSession(ctx, connect.NewRequest(&cagentv1.DeleteSessionRequest{Id: id}))
	return err
}

// ResumeSession resumes a session by ID with an optional rejection reason
func (c *ConnectRPCClient) ResumeSession(ctx context.Context, id, confirmation, reason string) error {
	_, err := c.client.ResumeSession(ctx, connect.NewRequest(&cagentv1.ResumeSessionRequest{
		Id:           id,
		Confirmation: confirmation,
		Reason:       reason,
	}))
	return err
}

// ToggleToolApproval toggles tool approval for a session
func (c *ConnectRPCClient) ToggleToolApproval(ctx context.Context, sessionID string) error {
	_, err := c.client.ToggleToolApproval(ctx, connect.NewRequest(&cagentv1.ToggleToolApprovalRequest{
		SessionId: sessionID,
	}))
	return err
}

// UpdateSessionTitle updates the title of a session
func (c *ConnectRPCClient) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	_, err := c.client.UpdateSessionTitle(ctx, connect.NewRequest(&cagentv1.UpdateSessionTitleRequest{
		SessionId: sessionID,
		Title:     title,
	}))
	return err
}

// ResumeElicitation sends an elicitation response
func (c *ConnectRPCClient) ResumeElicitation(ctx context.Context, sessionID string, action tools.ElicitationAction, content map[string]any) error {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("failed to marshal content: %w", err)
	}

	_, err = c.client.ResumeElicitation(ctx, connect.NewRequest(&cagentv1.ResumeElicitationRequest{
		SessionId:   sessionID,
		Action:      string(action),
		ContentJson: contentJSON,
	}))
	return err
}

// RunAgent executes an agent and returns a channel of streaming events
func (c *ConnectRPCClient) RunAgent(ctx context.Context, sessionID, agent string, messages []api.Message) (<-chan Event, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, "", messages)
}

// RunAgentWithAgentName executes an agent with a specific agent name
func (c *ConnectRPCClient) RunAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message) (<-chan Event, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, agentName, messages)
}

func (c *ConnectRPCClient) runAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message) (<-chan Event, error) {
	pbMessages := make([]*cagentv1.InputMessage, len(messages))
	for i, m := range messages {
		pbMessages[i] = &cagentv1.InputMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	req := &cagentv1.RunAgentRequest{
		SessionId: sessionID,
		Agent:     agent,
		AgentName: agentName,
		Messages:  pbMessages,
	}

	stream, err := c.client.RunAgent(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("failed to start agent stream: %w", err)
	}

	eventChan := make(chan Event, 128)

	go func() {
		defer close(eventChan)

		for stream.Receive() {
			resp := stream.Msg()
			event := c.convertProtoEventToRuntimeEvent(resp)
			if event != nil {
				eventChan <- event
			}
		}

		if err := stream.Err(); err != nil && err != io.EOF {
			slog.Error("Stream error", "error", err)
			eventChan <- Error(fmt.Sprintf("stream error: %v", err))
		}
	}()

	return eventChan, nil
}

func (c *ConnectRPCClient) convertProtoEventToRuntimeEvent(e *cagentv1.Event) Event {
	if e == nil {
		return nil
	}

	switch ev := e.Event.(type) {
	case *cagentv1.Event_UserMessage:
		return &UserMessageEvent{
			Type:    "user_message",
			Message: ev.UserMessage.Message,
		}

	case *cagentv1.Event_StreamStarted:
		return &StreamStartedEvent{
			Type:         "stream_started",
			SessionID:    ev.StreamStarted.SessionId,
			AgentContext: AgentContext{AgentName: ev.StreamStarted.AgentName},
		}

	case *cagentv1.Event_StreamStopped:
		return &StreamStoppedEvent{
			Type:         "stream_stopped",
			SessionID:    ev.StreamStopped.SessionId,
			AgentContext: AgentContext{AgentName: ev.StreamStopped.AgentName},
		}

	case *cagentv1.Event_AgentChoice:
		return &AgentChoiceEvent{
			Type:         "agent_choice",
			Content:      ev.AgentChoice.Content,
			AgentContext: AgentContext{AgentName: ev.AgentChoice.AgentName},
		}

	case *cagentv1.Event_AgentChoiceReasoning:
		return &AgentChoiceReasoningEvent{
			Type:         "agent_choice_reasoning",
			Content:      ev.AgentChoiceReasoning.Content,
			AgentContext: AgentContext{AgentName: ev.AgentChoiceReasoning.AgentName},
		}

	case *cagentv1.Event_PartialToolCall:
		return &PartialToolCallEvent{
			Type:           "partial_tool_call",
			ToolCall:       convertProtoToolCall(ev.PartialToolCall.ToolCall),
			ToolDefinition: convertProtoTool(ev.PartialToolCall.ToolDefinition),
			AgentContext:   AgentContext{AgentName: ev.PartialToolCall.AgentName},
		}

	case *cagentv1.Event_ToolCall:
		return &ToolCallEvent{
			Type:           "tool_call",
			ToolCall:       convertProtoToolCall(ev.ToolCall.ToolCall),
			ToolDefinition: convertProtoTool(ev.ToolCall.ToolDefinition),
			AgentContext:   AgentContext{AgentName: ev.ToolCall.AgentName},
		}

	case *cagentv1.Event_ToolCallConfirmation:
		return &ToolCallConfirmationEvent{
			Type:           "tool_call_confirmation",
			ToolCall:       convertProtoToolCall(ev.ToolCallConfirmation.ToolCall),
			ToolDefinition: convertProtoTool(ev.ToolCallConfirmation.ToolDefinition),
			AgentContext:   AgentContext{AgentName: ev.ToolCallConfirmation.AgentName},
		}

	case *cagentv1.Event_ToolCallResponse:
		var result *tools.ToolCallResult
		if ev.ToolCallResponse.Result != nil {
			var meta map[string]any
			_ = json.Unmarshal(ev.ToolCallResponse.Result.MetaJson, &meta)
			result = &tools.ToolCallResult{
				Output:  ev.ToolCallResponse.Result.Output,
				IsError: ev.ToolCallResponse.Result.IsError,
				Meta:    meta,
			}
		}
		return &ToolCallResponseEvent{
			Type:           "tool_call_response",
			ToolCall:       convertProtoToolCall(ev.ToolCallResponse.ToolCall),
			ToolDefinition: convertProtoTool(ev.ToolCallResponse.ToolDefinition),
			Response:       ev.ToolCallResponse.Response,
			Result:         result,
			AgentContext:   AgentContext{AgentName: ev.ToolCallResponse.AgentName},
		}

	case *cagentv1.Event_Error:
		return &ErrorEvent{
			Type:         "error",
			Error:        ev.Error.Error,
			AgentContext: AgentContext{AgentName: ev.Error.AgentName},
		}

	case *cagentv1.Event_Warning:
		return &WarningEvent{
			Type:         "warning",
			Message:      ev.Warning.Message,
			AgentContext: AgentContext{AgentName: ev.Warning.AgentName},
		}

	case *cagentv1.Event_TokenUsage:
		var usage *Usage
		if ev.TokenUsage.Usage != nil {
			usage = &Usage{
				InputTokens:   ev.TokenUsage.Usage.InputTokens,
				OutputTokens:  ev.TokenUsage.Usage.OutputTokens,
				ContextLength: ev.TokenUsage.Usage.ContextLength,
				ContextLimit:  ev.TokenUsage.Usage.ContextLimit,
				Cost:          ev.TokenUsage.Usage.Cost,
				LastMessage:   convertProtoMessageUsage(ev.TokenUsage.Usage.LastMessage),
			}
		}
		return &TokenUsageEvent{
			Type:         "token_usage",
			SessionID:    ev.TokenUsage.SessionId,
			Usage:        usage,
			AgentContext: AgentContext{AgentName: ev.TokenUsage.AgentName},
		}

	case *cagentv1.Event_SessionTitle:
		return &SessionTitleEvent{
			Type:         "session_title",
			SessionID:    ev.SessionTitle.SessionId,
			Title:        ev.SessionTitle.Title,
			AgentContext: AgentContext{AgentName: ev.SessionTitle.AgentName},
		}

	case *cagentv1.Event_SessionSummary:
		return &SessionSummaryEvent{
			Type:         "session_summary",
			SessionID:    ev.SessionSummary.SessionId,
			Summary:      ev.SessionSummary.Summary,
			AgentContext: AgentContext{AgentName: ev.SessionSummary.AgentName},
		}

	case *cagentv1.Event_SessionCompaction:
		return &SessionCompactionEvent{
			Type:         "session_compaction",
			SessionID:    ev.SessionCompaction.SessionId,
			Status:       ev.SessionCompaction.Status,
			AgentContext: AgentContext{AgentName: ev.SessionCompaction.AgentName},
		}

	case *cagentv1.Event_ElicitationRequest:
		var schema map[string]any
		_ = json.Unmarshal(ev.ElicitationRequest.SchemaJson, &schema)
		var meta map[string]any
		_ = json.Unmarshal(ev.ElicitationRequest.MetaJson, &meta)
		return &ElicitationRequestEvent{
			Type:         "elicitation_request",
			Message:      ev.ElicitationRequest.Message,
			Schema:       schema,
			Meta:         meta,
			AgentContext: AgentContext{AgentName: ev.ElicitationRequest.AgentName},
		}

	case *cagentv1.Event_Authorization:
		return &AuthorizationEvent{
			Type:         "authorization_event",
			Confirmation: tools.ElicitationAction(ev.Authorization.Confirmation),
			AgentContext: AgentContext{AgentName: ev.Authorization.AgentName},
		}

	case *cagentv1.Event_MaxIterationsReached:
		return &MaxIterationsReachedEvent{
			Type:          "max_iterations_reached",
			MaxIterations: int(ev.MaxIterationsReached.MaxIterations),
			AgentContext:  AgentContext{AgentName: ev.MaxIterationsReached.AgentName},
		}

	case *cagentv1.Event_McpInitStarted:
		return &MCPInitStartedEvent{
			Type:         "mcp_init_started",
			AgentContext: AgentContext{AgentName: ev.McpInitStarted.AgentName},
		}

	case *cagentv1.Event_McpInitFinished:
		return &MCPInitFinishedEvent{
			Type:         "mcp_init_finished",
			AgentContext: AgentContext{AgentName: ev.McpInitFinished.AgentName},
		}

	case *cagentv1.Event_AgentInfo:
		return &AgentInfoEvent{
			Type:           "agent_info",
			AgentName:      ev.AgentInfo.AgentName,
			Model:          ev.AgentInfo.Model,
			Description:    ev.AgentInfo.Description,
			WelcomeMessage: ev.AgentInfo.WelcomeMessage,
			AgentContext:   AgentContext{AgentName: ev.AgentInfo.AgentName},
		}

	case *cagentv1.Event_TeamInfo:
		agents := make([]AgentDetails, len(ev.TeamInfo.AvailableAgents))
		for i, a := range ev.TeamInfo.AvailableAgents {
			agents[i] = AgentDetails{
				Name:        a.Name,
				Description: a.Description,
				Provider:    a.Provider,
				Model:       a.Model,
			}
		}
		return &TeamInfoEvent{
			Type:            "team_info",
			AvailableAgents: agents,
			CurrentAgent:    ev.TeamInfo.CurrentAgent,
			AgentContext:    AgentContext{AgentName: ev.TeamInfo.AgentName},
		}

	case *cagentv1.Event_AgentSwitching:
		return &AgentSwitchingEvent{
			Type:         "agent_switching",
			Switching:    ev.AgentSwitching.Switching,
			FromAgent:    ev.AgentSwitching.FromAgent,
			ToAgent:      ev.AgentSwitching.ToAgent,
			AgentContext: AgentContext{AgentName: ev.AgentSwitching.AgentName},
		}

	case *cagentv1.Event_ToolsetInfo:
		return &ToolsetInfoEvent{
			Type:           "toolset_info",
			AvailableTools: int(ev.ToolsetInfo.AvailableTools),
			Loading:        ev.ToolsetInfo.Loading,
			AgentContext:   AgentContext{AgentName: ev.ToolsetInfo.AgentName},
		}

	case *cagentv1.Event_RagIndexingStarted:
		return &RAGIndexingStartedEvent{
			Type:         "rag_indexing_started",
			RAGName:      ev.RagIndexingStarted.RagName,
			StrategyName: ev.RagIndexingStarted.StrategyName,
			AgentContext: AgentContext{AgentName: ev.RagIndexingStarted.AgentName},
		}

	case *cagentv1.Event_RagIndexingProgress:
		return &RAGIndexingProgressEvent{
			Type:         "rag_indexing_progress",
			RAGName:      ev.RagIndexingProgress.RagName,
			StrategyName: ev.RagIndexingProgress.StrategyName,
			Current:      int(ev.RagIndexingProgress.Current),
			Total:        int(ev.RagIndexingProgress.Total),
			AgentContext: AgentContext{AgentName: ev.RagIndexingProgress.AgentName},
		}

	case *cagentv1.Event_RagIndexingCompleted:
		return &RAGIndexingCompletedEvent{
			Type:         "rag_indexing_completed",
			RAGName:      ev.RagIndexingCompleted.RagName,
			StrategyName: ev.RagIndexingCompleted.StrategyName,
			AgentContext: AgentContext{AgentName: ev.RagIndexingCompleted.AgentName},
		}

	case *cagentv1.Event_HookBlocked:
		return &HookBlockedEvent{
			Type:           "hook_blocked",
			ToolCall:       convertProtoToolCall(ev.HookBlocked.ToolCall),
			ToolDefinition: convertProtoTool(ev.HookBlocked.ToolDefinition),
			Message:        ev.HookBlocked.Message,
			AgentContext:   AgentContext{AgentName: ev.HookBlocked.AgentName},
		}

	case *cagentv1.Event_ShellOutput:
		return &ShellOutputEvent{
			Type:   "shell",
			Output: ev.ShellOutput.Output,
		}

	default:
		slog.Debug("Unknown event type from Connect-RPC stream", "event", e)
		return nil
	}
}

func convertProtoToolCall(tc *cagentv1.ToolCall) tools.ToolCall {
	if tc == nil {
		return tools.ToolCall{}
	}
	var fn tools.FunctionCall
	if tc.Function != nil {
		fn = tools.FunctionCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	}
	return tools.ToolCall{
		ID:       tc.Id,
		Type:     tools.ToolType(tc.Type),
		Function: fn,
	}
}

func convertProtoMessageUsage(m *cagentv1.LastMessageUsage) *MessageUsage {
	if m == nil {
		return nil
	}
	return &MessageUsage{
		Usage: chat.Usage{
			InputTokens:       m.InputTokens,
			OutputTokens:      m.OutputTokens,
			CachedInputTokens: m.CachedInputTokens,
			CacheWriteTokens:  m.CacheWriteTokens,
		},
		Cost:  m.Cost,
		Model: m.Model,
	}
}

func convertProtoTool(t *cagentv1.Tool) tools.Tool {
	if t == nil {
		return tools.Tool{}
	}

	var params map[string]any
	_ = json.Unmarshal(t.ParametersJson, &params)

	var outputSchema map[string]any
	_ = json.Unmarshal(t.OutputSchemaJson, &outputSchema)

	var annotations tools.ToolAnnotations
	if t.Annotations != nil {
		destructive := t.Annotations.DestructiveHint
		openWorld := t.Annotations.OpenWorldHint
		annotations = tools.ToolAnnotations{
			ReadOnlyHint:    t.Annotations.ReadOnlyHint,
			DestructiveHint: &destructive,
			IdempotentHint:  t.Annotations.IdempotentHint,
			OpenWorldHint:   &openWorld,
		}
	}

	return tools.Tool{
		Name:         t.Name,
		Category:     t.Category,
		Description:  t.Description,
		Parameters:   params,
		Annotations:  annotations,
		OutputSchema: outputSchema,
	}
}
