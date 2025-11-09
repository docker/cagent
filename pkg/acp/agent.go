package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coder/acp-go-sdk"
	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
)

// Agent implements the ACP Agent interface for cagent
type Agent struct {
	conn          *acp.AgentSideConnection
	team          *team.Team
	agentFilename string
	runtimeConfig config.RuntimeConfig
	sessions      map[string]*Session
	mu            sync.Mutex
}

var _ acp.Agent = (*Agent)(nil)

// Session represents an ACP session
type Session struct {
	id     string
	sess   *session.Session
	rt     runtime.Runtime
	cancel context.CancelFunc
}

// NewAgent creates a new ACP agent
func NewAgent(agentFilename string, runtimeConfig config.RuntimeConfig) *Agent {
	agent := &Agent{
		agentFilename: agentFilename,
		runtimeConfig: runtimeConfig,
		sessions:      make(map[string]*Session),
	}
	return agent
}

// Stop stops the agent and its toolsets
func (a *Agent) Stop(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.team != nil {
		if err := a.team.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}
}

// SetAgentConnection sets the ACP connection
func (a *Agent) SetAgentConnection(conn *acp.AgentSideConnection) {
	a.conn = conn
}

// Initialize implements [acp.Agent]
func (a *Agent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	slog.Debug("ACP Initialize called", "client_version", params.ProtocolVersion)

	a.mu.Lock()
	defer a.mu.Unlock()
	slog.Debug("Loading teams", "agent_file", a.agentFilename)
	t, err := teamloader.Load(ctx, a.agentFilename, a.runtimeConfig, teamloader.WithToolsetRegistry(createToolsetRegistry(a)))
	if err != nil {
		return acp.InitializeResponse{}, fmt.Errorf("failed to load teams: %w", err)
	}
	a.team = t
	slog.Debug("Teams loaded successfully", "team_id", t.ID, "agent_count", t.Size())

	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false,
			PromptCapabilities: acp.PromptCapabilities{
				EmbeddedContext: true,
			},
		},
	}, nil
}

// NewSession implements [acp.Agent]
func (a *Agent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := uuid.New().String()
	slog.Debug("ACP NewSession called", "session_id", sid)

	rt, err := runtime.New(a.team, runtime.WithCurrentAgent("root"))
	if err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("failed to create runtime: %w", err)
	}

	a.mu.Lock()
	a.sessions[sid] = &Session{
		id:   sid,
		sess: session.New(session.WithTitle("ACP Session " + sid)),
		rt:   rt,
	}
	a.mu.Unlock()

	return acp.NewSessionResponse{SessionId: acp.SessionId(sid)}, nil
}

// Authenticate implements [acp.Agent]
func (a *Agent) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	slog.Debug("ACP Authenticate called")
	return acp.AuthenticateResponse{}, nil
}

// LoadSession implements [acp.Agent] (optional, not supported)
func (a *Agent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	slog.Debug("ACP LoadSession called (not supported)")
	return acp.LoadSessionResponse{}, fmt.Errorf("load session not supported")
}

// Cancel implements [acp.Agent]
func (a *Agent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	sid := string(params.SessionId)
	slog.Debug("ACP Cancel called", "session_id", sid)

	a.mu.Lock()
	acpSess, ok := a.sessions[sid]
	a.mu.Unlock()

	if ok && acpSess != nil && acpSess.cancel != nil {
		acpSess.cancel()
	}

	return nil
}

// Prompt implements [acp.Agent]
func (a *Agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)
	slog.Debug("ACP Prompt called", "session_id", sid)

	a.mu.Lock()
	acpSess, ok := a.sessions[sid]
	a.mu.Unlock()

	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sid)
	}

	// Cancel any previous turn
	a.mu.Lock()
	if acpSess.cancel != nil {
		prev := acpSess.cancel
		a.mu.Unlock()
		prev()
	} else {
		a.mu.Unlock()
	}

	// Create a new context for this turn
	turnCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	acpSess.cancel = cancel
	a.mu.Unlock()

	// Add the user message to the session
	var userContent string
	for _, content := range params.Prompt {
		if content.Text != nil {
			userContent += content.Text.Text
		}
		if content.ResourceLink != nil {
			slog.Debug("resource link", "link", content.ResourceLink)
		}
		if content.Resource != nil {
			slog.Debug("embedded context", "context", content.Resource)
			slog.Debug(content.Resource.Resource.TextResourceContents.Text)
		}
	}

	if userContent != "" {
		acpSess.sess.AddMessage(session.UserMessage(a.agentFilename, userContent))
	}

	// Run the agent and stream updates
	if err := a.runAgent(turnCtx, acpSess); err != nil {
		if turnCtx.Err() != nil {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		return acp.PromptResponse{}, err
	}

	a.mu.Lock()
	acpSess.cancel = nil
	a.mu.Unlock()

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// SetSessionMode implements acp.Agent (optional)
func (a *Agent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	// We don't implement session modes, cagent agents have only one mode (for now? ;) ).
	return acp.SetSessionModeResponse{}, nil
}

// runAgent runs a single agent loop and streams updates to the ACP client
func (a *Agent) runAgent(ctx context.Context, acpSess *Session) error {
	slog.Debug("Running agent turn", "session_id", acpSess.id)

	ctx = withSessionID(ctx, acpSess.id)

	eventsChan := acpSess.rt.RunStream(ctx, acpSess.sess)

	for event := range eventsChan {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch e := event.(type) {
		case *runtime.AgentChoiceEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    acp.UpdateAgentMessageText(e.Content),
			}); err != nil {
				return err
			}

		case *runtime.ToolCallConfirmationEvent:
			if err := a.handleToolCallConfirmation(ctx, acpSess, e); err != nil {
				return err
			}

		case *runtime.ToolCallEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    buildToolCallStart(e.ToolCall, e.ToolDefinition),
			}); err != nil {
				return err
			}

		case *runtime.ToolCallResponseEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    buildToolCallComplete(e.ToolCall, e.Response),
			}); err != nil {
				return err
			}

		case *runtime.ErrorEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    acp.UpdateAgentMessageText(fmt.Sprintf("\n\nError: %s\n", e.Error)),
			}); err != nil {
				return err
			}

		case *runtime.MaxIterationsReachedEvent:
			if err := a.handleMaxIterationsReached(ctx, acpSess, e); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleToolCallConfirmation handles tool call permission requests
func (a *Agent) handleToolCallConfirmation(ctx context.Context, acpSess *Session, e *runtime.ToolCallConfirmationEvent) error {
	toolCallUpdate := buildToolCallUpdate(e.ToolCall, e.ToolDefinition, acp.ToolCallStatusPending)

	permResp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSess.id),
		ToolCall:  toolCallUpdate,
		Options: []acp.PermissionOption{
			{
				Kind:     acp.PermissionOptionKindAllowOnce,
				Name:     "Allow this action",
				OptionId: acp.PermissionOptionId("allow"),
			},
			{
				Kind:     acp.PermissionOptionKindAllowAlways,
				Name:     "Allow and remember my choice",
				OptionId: acp.PermissionOptionId("allow-always"),
			},
			{
				Kind:     acp.PermissionOptionKindRejectOnce,
				Name:     "Skip this action",
				OptionId: acp.PermissionOptionId("reject"),
			},
		},
	})
	if err != nil {
		return err
	}

	// Handle permission outcome
	if permResp.Outcome.Cancelled != nil {
		acpSess.rt.Resume(ctx, runtime.ResumeTypeReject)
		return nil
	}

	if permResp.Outcome.Selected == nil {
		return fmt.Errorf("unexpected permission outcome")
	}

	switch string(permResp.Outcome.Selected.OptionId) {
	case "allow":
		acpSess.rt.Resume(ctx, runtime.ResumeTypeApprove)
	case "allow-always":
		acpSess.rt.Resume(ctx, runtime.ResumeTypeApproveSession)
	case "reject":
		acpSess.rt.Resume(ctx, runtime.ResumeTypeReject)
	default:
		return fmt.Errorf("unexpected permission option: %s", permResp.Outcome.Selected.OptionId)
	}

	return nil
}

// handleMaxIterationsReached handles max iterations events
func (a *Agent) handleMaxIterationsReached(ctx context.Context, acpSess *Session, e *runtime.MaxIterationsReachedEvent) error {
	permResp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSess.id),
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: acp.ToolCallId("max_iterations"),
			Title:      acp.Ptr(fmt.Sprintf("Maximum iterations (%d) reached", e.MaxIterations)),
			Kind:       acp.Ptr(acp.ToolKindExecute),
			Status:     acp.Ptr(acp.ToolCallStatusPending),
		},
		Options: []acp.PermissionOption{
			{
				Kind:     acp.PermissionOptionKindAllowOnce,
				Name:     "Continue",
				OptionId: acp.PermissionOptionId("continue"),
			},
			{
				Kind:     acp.PermissionOptionKindRejectOnce,
				Name:     "Stop",
				OptionId: acp.PermissionOptionId("stop"),
			},
		},
	})
	if err != nil {
		return err
	}

	if permResp.Outcome.Cancelled != nil || permResp.Outcome.Selected == nil ||
		string(permResp.Outcome.Selected.OptionId) == "stop" {
		acpSess.rt.Resume(ctx, runtime.ResumeTypeReject)
	} else {
		acpSess.rt.Resume(ctx, runtime.ResumeTypeApprove)
	}

	return nil
}

// buildToolCallStart creates a tool call start update
func buildToolCallStart(toolCall tools.ToolCall, tool tools.Tool) acp.SessionUpdate {
	kind := acp.ToolKindExecute
	title := tool.Annotations.Title
	if title == "" {
		title = toolCall.Function.Name
	}

	// Determine tool kind from tool annotations
	if tool.Annotations.ReadOnlyHint {
		kind = acp.ToolKindRead
	}

	return acp.StartToolCall(
		acp.ToolCallId(toolCall.ID),
		title,
		acp.WithStartKind(kind),
		acp.WithStartStatus(acp.ToolCallStatusPending),
		acp.WithStartRawInput(parseToolCallArguments(toolCall.Function.Arguments)),
	)
}

// buildToolCallComplete creates a tool call completion update
func buildToolCallComplete(toolCall tools.ToolCall, output string) acp.SessionUpdate {
	return acp.UpdateToolCall(
		acp.ToolCallId(toolCall.ID),
		acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
		acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock(output))}),
		acp.WithUpdateRawOutput(map[string]any{"content": output}),
	)
}

// buildToolCallUpdate creates a tool call update for permission requests
func buildToolCallUpdate(toolCall tools.ToolCall, tool tools.Tool, status acp.ToolCallStatus) acp.RequestPermissionToolCall {
	kind := acp.ToolKindExecute
	title := tool.Annotations.Title
	if title == "" {
		title = toolCall.Function.Name
	}

	if tool.Annotations.ReadOnlyHint {
		kind = acp.ToolKindRead
	}

	return acp.RequestPermissionToolCall{
		ToolCallId: acp.ToolCallId(toolCall.ID),
		Title:      acp.Ptr(title),
		Kind:       acp.Ptr(kind),
		Status:     acp.Ptr(status),
		RawInput:   parseToolCallArguments(toolCall.Function.Arguments),
	}
}

// parseToolCallArguments parses JSON tool call arguments into a map
func parseToolCallArguments(argsJSON string) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		slog.Warn("Failed to parse tool call arguments", "error", err)
		return map[string]any{"raw": argsJSON}
	}
	return args
}
