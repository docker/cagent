package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tools"
)

type ResumeType string

type modelStore interface {
	GetModel(ctx context.Context, modelID string) (*modelsdev.Model, error)
}

// ElicitationResult represents the result of an elicitation request
type ElicitationResult struct {
	Action  string         // "accept", "decline", or "cancel"
	Content map[string]any // The submitted form data (only present when action is "accept")
}

// ElicitationError represents an error from a declined/cancelled elicitation
type ElicitationError struct {
	Action  string
	Message string
}

func (e *ElicitationError) Error() string {
	return fmt.Sprintf("elicitation %s: %s", e.Action, e.Message)
}

const (
	ResumeTypeApprove        ResumeType = "approve"
	ResumeTypeApproveSession ResumeType = "approve-session"
	ResumeTypeReject         ResumeType = "reject"
)

// ToolHandler is a function type for handling tool calls
type ToolHandler func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error)

// ElicitationRequestHandler is a function type for handling elicitation requests
type ElicitationRequestHandler func(ctx context.Context, message string, schema map[string]any) (map[string]any, error)

// Runtime defines the contract for runtime execution
type Runtime interface {
	// CurrentAgent returns the currently active agent
	CurrentAgent() *agent.Agent
	// StopPendingProcesses stops all pending tool operations (e.g., running shell commands)
	StopPendingProcesses(ctx context.Context) error
	// RunStream starts the agent's interaction loop and returns a channel of events
	RunStream(ctx context.Context, sess *session.Session) <-chan Event
	// Run starts the agent's interaction loop and returns the final messages
	Run(ctx context.Context, sess *session.Session) ([]session.Message, error)
	// Resume allows resuming execution after user confirmation
	Resume(ctx context.Context, confirmationType string)
	// Summarize generates a summary for the session
	Summarize(ctx context.Context, sess *session.Session, events chan Event)
	// ResumeElicitation sends an elicitation response back to a waiting elicitation request
	ResumeElicitation(_ context.Context, action string, content map[string]any) error
}

// runtime manages the execution of agents
type runtime struct {
	toolMap                     map[string]ToolHandler
	team                        *team.Team
	currentAgent                string
	rootSessionID               string // Root session ID for OAuth state encoding (preserved across sub-sessions)
	resumeChan                  chan ResumeType
	tracer                      trace.Tracer
	modelsStore                 modelStore
	sessionCompaction           bool
	managedOAuth                bool
	elicitationRequestCh        chan ElicitationResult // Channel for receiving elicitation responses
	elicitationEventsChannel    chan Event             // Current events channel for sending elicitation requests
	elicitationEventsChannelMux sync.RWMutex           // Protects elicitationEventsChannel
}

type streamResult struct {
	Calls             []tools.ToolCall
	Content           string
	ReasoningContent  string
	ThinkingSignature string // Used with Anthropic's extended thinking feature
	Stopped           bool
}

type Opt func(*runtime)

func WithCurrentAgent(agentName string) Opt {
	return func(r *runtime) {
		r.currentAgent = agentName
	}
}

func WithManagedOAuth(managed bool) Opt {
	return func(r *runtime) {
		r.managedOAuth = managed
	}
}

func WithRootSessionID(sessionID string) Opt {
	return func(r *runtime) {
		r.rootSessionID = sessionID
	}
}

// WithTracer sets a custom OpenTelemetry tracer; if not provided, tracing is disabled (no-op).
func WithTracer(t trace.Tracer) Opt {
	return func(r *runtime) {
		r.tracer = t
	}
}

func WithSessionCompaction(sessionCompaction bool) Opt {
	return func(r *runtime) {
		r.sessionCompaction = sessionCompaction
	}
}

func WithModelStore(store modelStore) Opt {
	return func(r *runtime) {
		r.modelsStore = store
	}
}

// New creates a new runtime for an agent and its team
func New(agents *team.Team, opts ...Opt) (Runtime, error) {
	modelsStore, err := modelsdev.NewStore()
	if err != nil {
		return nil, err
	}

	r := &runtime{
		toolMap:              make(map[string]ToolHandler),
		team:                 agents,
		currentAgent:         "root",
		resumeChan:           make(chan ResumeType),
		elicitationRequestCh: make(chan ElicitationResult),
		modelsStore:          modelsStore,
		sessionCompaction:    true,
		managedOAuth:         true,
	}

	for _, opt := range opts {
		opt(r)
	}

	// Validate that we have at least one agent and that the current agent exists
	if r.team == nil || r.team.Size() == 0 {
		return nil, fmt.Errorf("no agents loaded; ensure your agent configuration defines at least one agent")
	}
	if r.team.Agent(r.currentAgent) == nil {
		names := strings.Join(r.team.AgentNames(), ", ")
		if names == "" {
			names = "(none)"
		}
		return nil, fmt.Errorf("agent %q not found in team; available agents: %s", r.currentAgent, names)
	}

	slog.Debug("Creating new runtime", "agent", r.currentAgent, "available_agents", agents.Size())

	return r, nil
}

func (r *runtime) CurrentAgent() *agent.Agent {
	return r.team.Agent(r.currentAgent)
}

func (r *runtime) StopPendingProcesses(ctx context.Context) error {
	return r.team.StopToolSets(ctx)
}

// registerDefaultTools registers the default tool handlers
func (r *runtime) registerDefaultTools() {
	slog.Debug("Registering default tools")
	r.toolMap["transfer_task"] = r.handleTaskTransfer
	slog.Debug("Registered default tools", "count", len(r.toolMap))
}

func (r *runtime) finalizeEventChannel(ctx context.Context, sess *session.Session, events chan Event) {
	defer close(events)

	events <- StreamStopped(sess.ID, r.currentAgent)

	telemetry.RecordSessionEnd(ctx)

	if sess.Title == "" && len(sess.GetAllMessages()) > 0 {
		r.generateSessionTitle(ctx, sess, events)
	}
}

// RunStream starts the agent's interaction loop and returns a channel of events
func (r *runtime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	slog.Debug("Starting runtime stream", "agent", r.currentAgent, "session_id", sess.ID)
	events := make(chan Event, 128)

	go func() {
		telemetry.RecordSessionStart(ctx, r.currentAgent, sess.ID)

		ctx, sessionSpan := r.startSpan(ctx, "runtime.session", trace.WithAttributes(
			attribute.String("agent", r.currentAgent),
			attribute.String("session.id", sess.ID),
		))
		defer sessionSpan.End()

		a := r.team.Agent(r.currentAgent)

		// Set the events channel for elicitation requests
		r.setElicitationEventsChannel(events)
		defer r.clearElicitationEventsChannel()

		// Set elicitation handler on all MCP toolsets before getting tools
		for _, toolset := range a.ToolSets() {
			toolset.SetElicitationHandler(r.elicitationHandler)
			toolset.SetOAuthSuccessHandler(func() {
				events <- Authorization("confirmed", r.currentAgent)
			})
		}

		agentTools, err := r.getTools(ctx, a, sessionSpan, events)
		if err != nil {
			events <- Error(fmt.Sprintf("failed to get tools: %v", err))
			return
		}

		messages := sess.GetMessages(a)
		if sess.SendUserMessage {
			events <- UserMessage(messages[len(messages)-1].Content)
		}

		events <- StreamStarted(sess.ID, a.Name())

		defer r.finalizeEventChannel(ctx, sess, events)

		r.registerDefaultTools()

		iteration := 0
		// Use a runtime copy of maxIterations so we don't modify the session's persistent config
		runtimeMaxIterations := sess.MaxIterations

		for {
			// Check iteration limit
			if runtimeMaxIterations > 0 && iteration >= runtimeMaxIterations {
				slog.Debug("Maximum iterations reached", "agent", a.Name(), "iterations", iteration, "max", runtimeMaxIterations)
				events <- MaxIterationsReached(runtimeMaxIterations)

				// Wait for user decision
				select {
				case resumeType := <-r.resumeChan:
					if resumeType == ResumeTypeApprove {
						slog.Debug("User chose to continue after max iterations", "agent", a.Name())
						runtimeMaxIterations = iteration + 10
					} else {
						slog.Debug("User chose to exit after max iterations", "agent", a.Name())
						// Synthesize a final assistant message so callers (e.g., parent agents)
						// receive a non-empty response and providers are not given empty tool outputs.
						assistantMessage := chat.Message{
							Role:      chat.MessageRoleAssistant,
							Content:   fmt.Sprintf("I have reached the maximum number of iterations (%d). Stopping as requested by user.", runtimeMaxIterations),
							CreatedAt: time.Now().Format(time.RFC3339),
						}
						sess.AddMessage(session.NewAgentMessage(a, &assistantMessage))
						return
					}
				case <-ctx.Done():
					slog.Debug("Context cancelled while waiting for max iterations decision", "agent", a.Name())
					return
				}
			}
			iteration++
			// Exit immediately if the stream context has been cancelled (e.g., Ctrl+C)
			if err := ctx.Err(); err != nil {
				slog.Debug("Runtime stream context cancelled, stopping loop", "agent", a.Name(), "session_id", sess.ID)
				return
			}
			slog.Debug("Starting conversation loop iteration", "agent", a.Name())
			// Looping, get the updated messages from the session
			messages := sess.GetMessages(a)
			slog.Debug("Retrieved messages for processing", "agent", a.Name(), "message_count", len(messages))

			streamCtx, streamSpan := r.startSpan(ctx, "runtime.stream", trace.WithAttributes(
				attribute.String("agent", a.Name()),
				attribute.String("session.id", sess.ID),
			))

			model := a.Model()
			modelID := model.ID()
			slog.Debug("Using agent", "agent", a.Name(), "model", modelID)
			slog.Debug("Getting model definition", "model_id", modelID)
			m, err := r.modelsStore.GetModel(ctx, modelID)
			if err != nil {
				slog.Debug("Failed to get model definition", "error", err)
			}

			slog.Debug("Creating chat completion stream", "agent", a.Name())
			stream, err := model.CreateChatCompletionStream(streamCtx, messages, agentTools)
			if err != nil {
				streamSpan.RecordError(err)
				streamSpan.SetStatus(codes.Error, "creating chat completion")
				slog.Error("Failed to create chat completion stream", "agent", a.Name(), "error", err)
				// Track error in telemetry
				telemetry.RecordError(ctx, err.Error())
				events <- Error(fmt.Sprintf("creating chat completion: %v", err))
				streamSpan.End()
				return
			}

			slog.Debug("Processing stream", "agent", a.Name())
			res, err := r.handleStream(ctx, stream, a, agentTools, sess, m, events)
			if err != nil {
				// Treat context cancellation as a graceful stop
				if errors.Is(err, context.Canceled) {
					slog.Debug("Model stream canceled by context", "agent", a.Name(), "session_id", sess.ID)
					streamSpan.End()
					return
				}
				streamSpan.RecordError(err)
				streamSpan.SetStatus(codes.Error, "error handling stream")
				slog.Error("Error handling stream", "agent", a.Name(), "error", err)
				// Track error in telemetry
				telemetry.RecordError(ctx, err.Error())
				events <- Error(err.Error())
				streamSpan.End()
				return
			}
			streamSpan.SetAttributes(
				attribute.Int("tool.calls", len(res.Calls)),
				attribute.Int("content.length", len(res.Content)),
				attribute.Bool("stopped", res.Stopped),
			)
			streamSpan.End()
			slog.Debug("Stream processed", "agent", a.Name(), "tool_calls", len(res.Calls), "content_length", len(res.Content), "stopped", res.Stopped)

			// Add assistant message to conversation history, but skip empty assistant messages
			// Providers reject assistant messages that have neither content nor tool calls.
			if strings.TrimSpace(res.Content) != "" || len(res.Calls) > 0 {
				assistantMessage := chat.Message{
					Role:              chat.MessageRoleAssistant,
					Content:           res.Content,
					ReasoningContent:  res.ReasoningContent,
					ThinkingSignature: res.ThinkingSignature,
					ToolCalls:         res.Calls,
					CreatedAt:         time.Now().Format(time.RFC3339),
				}

				sess.AddMessage(session.NewAgentMessage(a, &assistantMessage))
				slog.Debug("Added assistant message to session", "agent", a.Name(), "total_messages", len(sess.GetAllMessages()))
			} else {
				slog.Debug("Skipping empty assistant message (no content and no tool calls)", "agent", a.Name())
			}

			contextLimit := 0
			if m != nil {
				contextLimit = m.Limit.Context
			}
			events <- TokenUsage(sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)

			if m != nil && r.sessionCompaction {
				if sess.InputTokens+sess.OutputTokens > int(float64(contextLimit)*0.9) {
					// Avoid inserting a summary between assistant tool_use and tool_result messages.
					// Defer compaction until after tool calls are processed in this iteration.
					if len(res.Calls) == 0 {
						events <- SessionCompaction(sess.ID, "start", r.currentAgent)
						r.Summarize(ctx, sess, events)
						events <- TokenUsage(sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)
						events <- SessionCompaction(sess.ID, "completed", r.currentAgent)
					}
				}
			}

			r.processToolCalls(ctx, sess, res.Calls, agentTools, events)

			// If tool_use occurred, perform compaction after tool results are appended
			// to avoid splitting assistant tool_use and user tool_result adjacency.
			if m != nil && r.sessionCompaction && len(res.Calls) > 0 {
				if sess.InputTokens+sess.OutputTokens > int(float64(contextLimit)*0.9) {
					events <- SessionCompaction(sess.ID, "start", r.currentAgent)
					r.Summarize(ctx, sess, events)
					events <- TokenUsage(sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)
					events <- SessionCompaction(sess.ID, "completed", r.currentAgent)
				}
			}

			if res.Stopped {
				slog.Debug("Conversation stopped", "agent", a.Name())
				break
			}
		}
	}()

	return events
}

func (r *runtime) getTools(ctx context.Context, a *agent.Agent, sessionSpan trace.Span, events chan Event) ([]tools.Tool, error) {
	// Execute tool retrieval with automatic OAuth handling
	var agentTools []tools.Tool
	shouldEmitMCPInit := events != nil && len(a.ToolSets()) > 0
	if shouldEmitMCPInit {
		events <- MCPInitStarted(a.Name())
	}
	defer func() {
		if shouldEmitMCPInit {
			events <- MCPInitFinished(a.Name())
		}
	}()

	tls, err := a.Tools(ctx)
	if err != nil {
		slog.Error("Failed to get agent tools", "agent", a.Name(), "error", err)
		sessionSpan.RecordError(err)
		sessionSpan.SetStatus(codes.Error, "failed to get tools")
		telemetry.RecordError(ctx, err.Error())
		return nil, err
	}
	agentTools = tls

	slog.Debug("Retrieved agent tools", "agent", a.Name(), "tool_count", len(agentTools))
	return agentTools, nil
}

func (r *runtime) Resume(_ context.Context, confirmationType string) {
	slog.Debug("Resuming runtime", "agent", r.currentAgent, "confirmation_type", confirmationType)

	cType := ResumeTypeApproveSession
	switch confirmationType {
	case "approve":
		cType = ResumeTypeApprove
	case "reject":
		cType = ResumeTypeReject
	}

	select {
	case r.resumeChan <- cType:
		slog.Debug("Resume signal sent", "agent", r.currentAgent)
	default:
		slog.Debug("Resume channel not ready, ignoring", "agent", r.currentAgent)
	}
}

// ResumeElicitation sends an elicitation response back to a waiting elicitation request
func (r *runtime) ResumeElicitation(ctx context.Context, action string, content map[string]any) error {
	slog.Debug("Resuming runtime with elicitation response", "agent", r.currentAgent, "action", action)

	result := ElicitationResult{
		Action:  action,
		Content: content,
	}

	select {
	case <-ctx.Done():
		slog.Debug("Context cancelled while sending elicitation response")
		return ctx.Err()
	case r.elicitationRequestCh <- result:
		slog.Debug("Elicitation response sent successfully", "action", action)
		return nil
	default:
		slog.Debug("Elicitation channel not ready")
		return fmt.Errorf("no elicitation request in progress")
	}
}

// Run starts the agent's interaction loop
func (r *runtime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}

func (r *runtime) handleStream(ctx context.Context, stream chat.MessageStream, a *agent.Agent, agentTools []tools.Tool, sess *session.Session, m *modelsdev.Model, events chan Event) (streamResult, error) {
	defer stream.Close()

	var fullContent strings.Builder
	var fullReasoningContent strings.Builder
	var thinkingSignature string
	var toolCalls []tools.ToolCall
	// Track which tool call indices we've already emitted partial events for
	emittedPartialEvents := make(map[string]bool)

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return streamResult{Stopped: true}, fmt.Errorf("error receiving from stream: %w", err)
		}

		if response.Usage != nil {
			if m != nil {
				sess.Cost += (float64(response.Usage.InputTokens)*m.Cost.Input +
					float64(response.Usage.OutputTokens+response.Usage.ReasoningTokens)*m.Cost.Output +
					float64(response.Usage.CachedInputTokens)*m.Cost.CacheRead +
					float64(response.Usage.CachedOutputTokens)*m.Cost.CacheWrite) / 1e6
			}

			sess.InputTokens = response.Usage.InputTokens + response.Usage.CachedInputTokens
			sess.OutputTokens = response.Usage.OutputTokens + response.Usage.CachedOutputTokens + response.Usage.ReasoningTokens

			modelName := "unknown"
			if m != nil {
				modelName = m.Name
			}
			telemetry.RecordTokenUsage(ctx, modelName, int64(response.Usage.InputTokens), int64(response.Usage.OutputTokens+response.Usage.ReasoningTokens), sess.Cost)
		}

		if len(response.Choices) == 0 {
			continue
		}
		choice := response.Choices[0]
		if choice.FinishReason == chat.FinishReasonStop || choice.FinishReason == chat.FinishReasonLength {
			return streamResult{
				Calls:             toolCalls,
				Content:           fullContent.String(),
				ReasoningContent:  fullReasoningContent.String(),
				ThinkingSignature: thinkingSignature,
				Stopped:           true,
			}, nil
		}

		// Handle tool calls
		if len(choice.Delta.ToolCalls) > 0 {
			// Process each tool call delta
			for _, deltaToolCall := range choice.Delta.ToolCalls {
				idx := 0
				for _, toolCall := range toolCalls {
					if toolCall.ID == deltaToolCall.ID {
						break
					}
					idx++
				}

				if idx >= len(toolCalls) {
					newToolCalls := make([]tools.ToolCall, idx+1)
					copy(newToolCalls, toolCalls)
					toolCalls = newToolCalls
				}

				// Check if we should emit a partial event for this tool call
				// We want to emit when we first get the function name
				shouldEmitPartial := !emittedPartialEvents[deltaToolCall.ID] &&
					deltaToolCall.Function.Name != "" &&
					toolCalls[idx].Function.Name == "" // Don't emit if we already have the name

				// Update fields based on what's in the delta
				if deltaToolCall.ID != "" {
					toolCalls[idx].ID = deltaToolCall.ID
				}
				if deltaToolCall.Type != "" {
					toolCalls[idx].Type = deltaToolCall.Type
				}
				if deltaToolCall.Function.Name != "" {
					toolCalls[idx].Function.Name = deltaToolCall.Function.Name
				}
				if deltaToolCall.Function.Arguments != "" {
					if toolCalls[idx].Function.Arguments == "" {
						toolCalls[idx].Function.Arguments = deltaToolCall.Function.Arguments
					} else {
						toolCalls[idx].Function.Arguments += deltaToolCall.Function.Arguments
					}
					// Emit if we get more arguments
					shouldEmitPartial = true
				}

				// Emit PartialToolCallEvent when we first get the function name
				if shouldEmitPartial {
					// TODO: clean this up, it's gross
					tool := tools.Tool{}
					for _, t := range agentTools {
						if t.Name == toolCalls[idx].Function.Name {
							tool = t
							break
						}
					}
					events <- PartialToolCall(toolCalls[idx], tool, a.Name())
					emittedPartialEvents[deltaToolCall.ID] = true
				}
			}
			continue
		}

		if choice.Delta.ReasoningContent != "" {
			events <- AgentChoiceReasoning(a.Name(), choice.Delta.ReasoningContent)
			fullReasoningContent.WriteString(choice.Delta.ReasoningContent)
		}

		// Capture thinking signature for Anthropic extended thinking
		if choice.Delta.ThinkingSignature != "" {
			thinkingSignature = choice.Delta.ThinkingSignature
		}

		if choice.Delta.Content != "" {
			events <- AgentChoice(a.Name(), choice.Delta.Content)
			fullContent.WriteString(choice.Delta.Content)
		}
	}

	// If the stream completed without producing any content or tool calls, likely because of a token limit, stop to avoid breaking the request loop
	// NOTE(krissetto): this can likely be removed once compaction works properly with all providers (aka dmr)
	stoppedDueToNoOutput := fullContent.Len() == 0 && len(toolCalls) == 0
	return streamResult{
		Calls:             toolCalls,
		Content:           fullContent.String(),
		ReasoningContent:  fullReasoningContent.String(),
		ThinkingSignature: thinkingSignature,
		Stopped:           stoppedDueToNoOutput,
	}, nil
}

// processToolCalls handles the execution of tool calls for an agent
func (r *runtime) processToolCalls(ctx context.Context, sess *session.Session, calls []tools.ToolCall, agentTools []tools.Tool, events chan Event) {
	a := r.CurrentAgent()
	slog.Debug("Processing tool calls", "agent", a.Name(), "call_count", len(calls))

	for i, toolCall := range calls {
		// Start a span for each tool call
		callCtx, callSpan := r.startSpan(ctx, "runtime.tool.call", trace.WithAttributes(
			attribute.String("tool.name", toolCall.Function.Name),
			attribute.String("tool.type", string(toolCall.Type)),
			attribute.String("agent", a.Name()),
			attribute.String("session.id", sess.ID),
			attribute.String("tool.call_id", toolCall.ID),
		))

		slog.Debug("Processing tool call", "agent", a.Name(), "tool", toolCall.Function.Name, "session_id", sess.ID)
		handler, exists := r.toolMap[toolCall.Function.Name]
		if exists {
			tool := tools.Tool{
				Annotations: tools.ToolAnnotations{
					// TODO: We need to handle the transfer task tool better
					Title: "Transfer Task",
				},
			}
			slog.Debug("Using runtime tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
			if sess.ToolsApproved || toolCall.Function.Name == "transfer_task" {
				r.runAgentTool(callCtx, handler, sess, toolCall, tool, events, a)
			} else {
				slog.Debug("Tools not approved, waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)

				events <- ToolCallConfirmation(toolCall, tool, a.Name())

				select {
				case cType := <-r.resumeChan:
					switch cType {
					case ResumeTypeApprove:
						slog.Debug("Resume signal received, approving tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.runAgentTool(callCtx, handler, sess, toolCall, tool, events, a)
					case ResumeTypeApproveSession:
						slog.Debug("Resume signal received, approving session", "tool", toolCall.Function.Name, "session_id", sess.ID)
						sess.ToolsApproved = true
						r.runAgentTool(callCtx, handler, sess, toolCall, tool, events, a)
					case ResumeTypeReject:
						slog.Debug("Resume signal received, rejecting tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.addToolRejectedResponse(sess, toolCall, tool, events)
					}
				case <-callCtx.Done():
					slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
					// Synthesize cancellation responses for the current and any remaining tool calls
					r.addToolCancelledResponse(sess, toolCall, tool, events)
					for j := i + 1; j < len(calls); j++ {
						r.addToolCancelledResponse(sess, calls[j], tool, events)
					}
					callSpan.SetStatus(codes.Ok, "tool call canceled by user")
					return
				}
			}
		}

	toolLoop:
		for _, tool := range agentTools {
			if _, ok := r.toolMap[tool.Name]; ok {
				continue
			}
			if tool.Name != toolCall.Function.Name {
				continue
			}
			slog.Debug("Using agent tool handler", "tool", toolCall.Function.Name)

			if sess.ToolsApproved || tool.Annotations.ReadOnlyHint {
				slog.Debug("Tools approved, running tool", "tool", toolCall.Function.Name, "session_id", sess.ID)
				r.runTool(callCtx, tool, toolCall, events, sess, a)
			} else {
				slog.Debug("Tools not approved, waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
				events <- ToolCallConfirmation(toolCall, tool, a.Name())
				select {
				case cType := <-r.resumeChan:
					switch cType {
					case ResumeTypeApprove:
						slog.Debug("Resume signal received, approving tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.runTool(callCtx, tool, toolCall, events, sess, a)
					case ResumeTypeApproveSession:
						slog.Debug("Resume signal received, approving session", "tool", toolCall.Function.Name, "session_id", sess.ID)
						sess.ToolsApproved = true
						r.runTool(callCtx, tool, toolCall, events, sess, a)
					case ResumeTypeReject:
						slog.Debug("Resume signal received, rejecting tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.addToolRejectedResponse(sess, toolCall, tool, events)
					}

					slog.Debug("Added tool response to session", "tool", toolCall.Function.Name, "session_id", sess.ID, "total_messages", len(sess.GetAllMessages()))
					break toolLoop
				case <-callCtx.Done():
					slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
					// Synthesize cancellation responses for the current and any remaining tool calls
					r.addToolCancelledResponse(sess, toolCall, tool, events)
					for j := i + 1; j < len(calls); j++ {
						r.addToolCancelledResponse(sess, calls[j], tool, events)
					}
					callSpan.SetStatus(codes.Ok, "tool call canceled by user")
					return
				}
			}
		}
		// Set tool call span success after processing corresponding handler
		callSpan.SetStatus(codes.Ok, "tool call processed")
		callSpan.End()
	}
}

// runTool executes agent tools from toolsets (MCP, filesystem, etc.).
// Tool execution may require OAuth authorization, so the handler call is wrapped
// with ExecuteWithOAuth to automatically handle authorization flows and retries.
func (r *runtime) runTool(ctx context.Context, tool tools.Tool, toolCall tools.ToolCall, events chan Event, sess *session.Session, a *agent.Agent) {
	// Start a child span for the actual tool handler execution
	ctx, span := r.startSpan(ctx, "runtime.tool.handler", trace.WithAttributes(
		attribute.String("tool.name", toolCall.Function.Name),
		attribute.String("agent", a.Name()),
		attribute.String("session.id", sess.ID),
		attribute.String("tool.call_id", toolCall.ID),
	))
	defer span.End()

	events <- ToolCall(toolCall, tool, a.Name())

	var res *tools.ToolCallResult
	var err error
	var duration time.Duration

	res, err = tool.Handler(ctx, toolCall)

	telemetry.RecordToolCall(ctx, toolCall.Function.Name, sess.ID, a.Name(), duration, err)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("Tool handler canceled by context", "tool", toolCall.Function.Name, "agent", a.Name(), "session_id", sess.ID)
			// Synthesize a cancellation response so the transcript remains consistent
			res = &tools.ToolCallResult{Output: "The tool call was canceled by the user."}
			span.SetStatus(codes.Ok, "tool handler canceled by user")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "tool handler error")
			slog.Error("Error calling tool", "tool", toolCall.Function.Name, "error", err)
			res = &tools.ToolCallResult{
				Output: fmt.Sprintf("Error calling tool: %v", err),
			}
		}
	} else {
		span.SetStatus(codes.Ok, "tool handler completed")
		slog.Debug("Agent tool call completed", "tool", toolCall.Function.Name, "output_length", len(res.Output))
	}

	events <- ToolCallResponse(toolCall, tool, res.Output, a.Name())

	// Ensure tool response content is not empty for API compatibility
	content := res.Output
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    content,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(a, &toolResponseMsg))
}

func (r *runtime) runAgentTool(ctx context.Context, handler ToolHandler, sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event, a *agent.Agent) {
	// Start a child span for runtime-provided tool handler execution
	ctx, span := r.startSpan(ctx, "runtime.tool.handler.runtime", trace.WithAttributes(
		attribute.String("tool.name", toolCall.Function.Name),
		attribute.String("agent", a.Name()),
		attribute.String("session.id", sess.ID),
		attribute.String("tool.call_id", toolCall.ID),
	))
	defer span.End()

	events <- ToolCall(toolCall, tool, a.Name())
	start := time.Now()
	res, err := handler(ctx, sess, toolCall, events)
	duration := time.Since(start)

	telemetry.RecordToolCall(ctx, toolCall.Function.Name, sess.ID, a.Name(), duration, err)

	var output string
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("Runtime tool handler canceled by context", "tool", toolCall.Function.Name, "agent", a.Name(), "session_id", sess.ID)
			// Synthesize a cancellation response so the transcript remains consistent
			output = "The tool call was canceled by the user."
			span.SetStatus(codes.Ok, "runtime tool handler canceled by user")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "runtime tool handler error")
			output = fmt.Sprintf("error calling tool: %v", err)
			slog.Error("Error executing tool", "tool", toolCall.Function.Name, "error", err)
		}
	} else {
		output = res.Output
		span.SetStatus(codes.Ok, "runtime tool handler completed")
		slog.Debug("Tool executed successfully", "tool", toolCall.Function.Name)
	}

	events <- ToolCallResponse(toolCall, tool, output, a.Name())

	// Ensure tool response content is not empty for API compatibility
	content := output
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    content,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(a, &toolResponseMsg))
}

func (r *runtime) addToolRejectedResponse(sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event) {
	a := r.CurrentAgent()

	result := "The user rejected the tool call."

	events <- ToolCallResponse(toolCall, tool, result, a.Name())

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(a, &toolResponseMsg))
}

func (r *runtime) addToolCancelledResponse(sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event) {
	a := r.CurrentAgent()

	result := "The tool call was canceled by the user."

	events <- ToolCallResponse(toolCall, tool, result, a.Name())

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(a, &toolResponseMsg))
}

// startSpan wraps tracer.Start, returning a no-op span if the tracer is nil.
func (r *runtime) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if r.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return r.tracer.Start(ctx, name, opts...)
}

func (r *runtime) handleTaskTransfer(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, evts chan Event) (*tools.ToolCallResult, error) {
	var params struct {
		Agent          string `json:"agent"`
		Task           string `json:"task"`
		ExpectedOutput string `json:"expected_output"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	a := r.CurrentAgent()

	// Span for task transfer (optional)
	ctx, span := r.startSpan(ctx, "runtime.task_transfer", trace.WithAttributes(
		attribute.String("from.agent", a.Name()),
		attribute.String("to.agent", params.Agent),
		attribute.String("session.id", sess.ID),
	))
	defer span.End()

	slog.Debug("Transferring task to agent", "from_agent", a.Name(), "to_agent", params.Agent, "task", params.Task)

	ca := r.currentAgent
	r.currentAgent = params.Agent
	defer func() { r.currentAgent = ca }()

	memberAgentTask := "You are a member of a team of agents. Your goal is to complete the following task:"
	memberAgentTask += fmt.Sprintf("\n\n<task>\n%s\n</task>", params.Task)
	if params.ExpectedOutput != "" {
		memberAgentTask += fmt.Sprintf("\n\n<expected_output>\n%s\n</expected_output>", params.ExpectedOutput)
	}

	slog.Debug("Creating new session with parent session", "parent_session_id", sess.ID, "tools_approved", sess.ToolsApproved)

	subAgentMaxIter := 0
	if child := r.team.Agent(params.Agent); child != nil {
		subAgentMaxIter = child.MaxIterations()
	}

	s := session.New(
		session.WithSystemMessage(memberAgentTask),
		session.WithImplicitUserMessage("", "Follow the default instructions"),
		session.WithMaxIterations(subAgentMaxIter),
	)
	s.SendUserMessage = false
	s.Title = "Transferred task"
	s.ToolsApproved = sess.ToolsApproved

	for event := range r.RunStream(ctx, s) {
		evts <- event
		if errEvent, ok := event.(*ErrorEvent); ok {
			span.RecordError(fmt.Errorf("%s", errEvent.Error))
			span.SetStatus(codes.Error, "error in transferred session")
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	sess.ToolsApproved = s.ToolsApproved
	sess.Cost += s.Cost

	sess.AddSubSession(s)

	slog.Debug("Task transfer completed", "agent", params.Agent, "task", params.Task)

	span.SetStatus(codes.Ok, "task transfer completed")
	return &tools.ToolCallResult{
		Output: s.GetLastAssistantMessageContent(),
	}, nil
}

// generateSessionTitle generates a title for the session based on the conversation history
func (r *runtime) generateSessionTitle(ctx context.Context, sess *session.Session, events chan Event) {
	slog.Debug("Generating title for session", "session_id", sess.ID)

	// Create conversation history summary
	var conversationHistory strings.Builder
	messages := sess.GetAllMessages()
	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			role = "System"
		}
		conversationHistory.WriteString(fmt.Sprintf("\n%s: %s", role, messages[i].Message.Content))
	}

	// Create a new session for title generation with auto-run tools
	systemPrompt := "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given a conversation history and asked to create a title that captures the main topic."
	userPrompt := fmt.Sprintf("Based on the following conversation between a user and an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text, nothing else.\n\nConversation history:%s\n\nGenerate a title for this conversation:", conversationHistory.String())

	titleModel := provider.CloneWithOptions(ctx, r.CurrentAgent().Model(), nil, options.WithStructuredOutput(nil))

	newTeam := team.New(
		team.WithID("title-generator"),
		team.WithAgents(agent.New("root", systemPrompt, agent.WithModel(titleModel))),
	)

	titleSession := session.New(session.WithSystemMessage(systemPrompt))
	titleSession.AddMessage(session.UserMessage("", userPrompt))
	titleSession.Title = "Generating title..."

	titleRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create title generator runtime", "error", err)
		return
	}

	// Run the title generation (this will be a simple back-and-forth)
	_, err = titleRuntime.Run(ctx, titleSession)
	if err != nil {
		slog.Error("Failed to generate session title", "session_id", sess.ID, "error", err)
		return
	}

	// Get the generated title from the last assistant message
	title := titleSession.GetLastAssistantMessageContent()
	if title == "" {
		return
	}
	sess.Title = title
	slog.Debug("Generated session title", "session_id", sess.ID, "title", title)
	events <- SessionTitle(sess.ID, title, r.currentAgent)
}

// Summarize generates a summary for the session based on the conversation history
func (r *runtime) Summarize(ctx context.Context, sess *session.Session, events chan Event) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", r.currentAgent)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", r.currentAgent)
	}()

	// Create conversation history for summarization
	var conversationHistory strings.Builder
	messages := sess.GetAllMessages()
	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			continue // Skip system messages for summarization
		}
		conversationHistory.WriteString(fmt.Sprintf("\n%s: %s", role, messages[i].Message.Content))
	}

	// Create a new session for summary generation
	systemPrompt := "You are a helpful AI assistant that creates comprehensive summaries of conversations. You will be given a conversation history and asked to create a concise yet thorough summary that captures the key points, decisions made, and outcomes."
	userPrompt := fmt.Sprintf("Based on the following conversation between a user and an AI assistant, create a comprehensive summary that captures:\n- The main topics discussed\n- Key information exchanged\n- Decisions made or conclusions reached\n- Important outcomes or results\n\nProvide a well-structured summary (2-4 paragraphs) that someone could read to understand what happened in this conversation. Return ONLY the summary text, nothing else.\n\nConversation history:%s\n\nGenerate a summary for this conversation:", conversationHistory.String())
	newModel := provider.CloneWithOptions(ctx, r.CurrentAgent().Model(), nil, options.WithStructuredOutput(nil))
	newTeam := team.New(
		team.WithID("summary-generator"),
		team.WithAgents(agent.New("root", systemPrompt, agent.WithModel(newModel))),
	)

	summarySession := session.New(session.WithSystemMessage(systemPrompt))
	summarySession.AddMessage(session.UserMessage("", userPrompt))
	summarySession.Title = "Generating summary..."

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create summary generator runtime", "error", err)
		return
	}

	// Run the summary generation
	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		slog.Error("Failed to generate session summary", "session_id", sess.ID, "error", err)
		return
	}

	summary := summarySession.GetLastAssistantMessageContent()
	if summary == "" {
		return
	}
	// Add the summary to the session as a summary item
	sess.Messages = append(sess.Messages, session.Item{Summary: summary})
	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, r.currentAgent)
}

// setElicitationEventsChannel sets the current events channel for elicitation requests
func (r *runtime) setElicitationEventsChannel(events chan Event) {
	r.elicitationEventsChannelMux.Lock()
	defer r.elicitationEventsChannelMux.Unlock()
	r.elicitationEventsChannel = events
}

// clearElicitationEventsChannel clears the current events channel
func (r *runtime) clearElicitationEventsChannel() {
	r.elicitationEventsChannelMux.Lock()
	defer r.elicitationEventsChannelMux.Unlock()
	r.elicitationEventsChannel = nil
}

// elicitationHandler creates an elicitation handler that can be used by MCP clients
// This handler propagates elicitation requests to the runtime's client via events
func (r *runtime) elicitationHandler(ctx context.Context, req *mcp.ElicitParams) (tools.ElicitationResult, error) {
	slog.Debug("Elicitation request received from MCP server", "message", req.Message)

	// Get the current events channel
	r.elicitationEventsChannelMux.RLock()
	eventsChannel := r.elicitationEventsChannel
	r.elicitationEventsChannelMux.RUnlock()

	if eventsChannel == nil {
		return tools.ElicitationResult{}, fmt.Errorf("no events channel available for elicitation")
	}

	slog.Debug("Sending elicitation request event to client", "message", req.Message, "requested_schema", req.RequestedSchema)
	slog.Debug("Elicitation request meta", "meta", req.Meta)

	// Send elicitation request event to the runtime's client
	eventsChannel <- ElicitationRequest(req.Message, req.RequestedSchema, req.Meta, r.currentAgent)

	// Wait for response from the client
	select {
	case result := <-r.elicitationRequestCh:
		return tools.ElicitationResult{
			Action:  result.Action,
			Content: result.Content,
		}, nil
	case <-ctx.Done():
		slog.Debug("Context cancelled while waiting for elicitation response")
		return tools.ElicitationResult{}, ctx.Err()
	}
}
