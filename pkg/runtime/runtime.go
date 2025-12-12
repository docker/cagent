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
	"sync/atomic"
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
	"github.com/docker/cagent/pkg/rag"
	ragtypes "github.com/docker/cagent/pkg/rag/types"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
)

type SessionStore interface {
	UpdateSession(ctx context.Context, sess *session.Session) error
}

// UnwrapMCPToolset extracts an MCP toolset from a potentially wrapped StartableToolSet.
// Returns the MCP toolset if found, or nil if the toolset is not an MCP toolset.
func UnwrapMCPToolset(toolset tools.ToolSet) *mcptools.Toolset {
	var innerToolset tools.ToolSet
	if startableTS, ok := toolset.(*agent.StartableToolSet); ok {
		innerToolset = startableTS.ToolSet
	} else {
		innerToolset = toolset
	}

	if mcpToolset, ok := innerToolset.(*mcptools.Toolset); ok {
		return mcpToolset
	}

	return nil
}

type ResumeType string

// ElicitationResult represents the result of an elicitation request
type ElicitationResult struct {
	Action  tools.ElicitationAction
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

// ToolHandlerFunc is a function type for handling tool calls
type ToolHandlerFunc func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error)

type ToolHandler struct {
	handler ToolHandlerFunc
	tool    tools.Tool
}

// ElicitationRequestHandler is a function type for handling elicitation requests
type ElicitationRequestHandler func(ctx context.Context, message string, schema map[string]any) (map[string]any, error)

// Runtime defines the contract for runtime execution
type Runtime interface {
	// CurrentAgentName returns the name of the currently active agent
	CurrentAgentName() string
	// CurrentAgentCommands returns the commands for the active agent
	CurrentAgentCommands(ctx context.Context) map[string]string
	// CurrentWelcomeMessage returns the welcome message for the active agent
	CurrentWelcomeMessage(ctx context.Context) string
	// EmitStartupInfo emits initial agent, team, and toolset information for immediate display
	EmitStartupInfo(ctx context.Context, events chan Event)

	// RunStream starts the agent's interaction loop and returns a channel of events
	RunStream(ctx context.Context, sess *session.Session) <-chan Event
	// Run starts the agent's interaction loop and returns the final messages
	Run(ctx context.Context, sess *session.Session) ([]session.Message, error)
	// Resume allows resuming execution after user confirmation
	Resume(ctx context.Context, confirmationType ResumeType)
	// ResumeElicitation sends an elicitation response back to a waiting elicitation request
	ResumeElicitation(_ context.Context, action tools.ElicitationAction, content map[string]any) error

	// Summarize generates a summary for the session
	Summarize(ctx context.Context, sess *session.Session, events chan Event)
}

type ModelStore interface {
	GetModel(ctx context.Context, modelID string) (*modelsdev.Model, error)
}

// RAGInitializer is implemented by runtimes that support background RAG initialization.
// Local runtimes use this to start indexing early; remote runtimes typically do not.
type RAGInitializer interface {
	StartBackgroundRAGInit(ctx context.Context, sendEvent func(Event))
}

// LocalRuntime manages the execution of agents
type LocalRuntime struct {
	toolMap                     map[string]ToolHandler
	team                        *team.Team
	currentAgent                string
	resumeChan                  chan ResumeType
	tracer                      trace.Tracer
	modelsStore                 ModelStore
	sessionCompaction           bool
	managedOAuth                bool
	startupInfoEmitted          bool                   // Track if startup info has been emitted to avoid unnecessary duplication
	elicitationRequestCh        chan ElicitationResult // Channel for receiving elicitation responses
	elicitationEventsChannel    chan Event             // Current events channel for sending elicitation requests
	elicitationEventsChannelMux sync.RWMutex           // Protects elicitationEventsChannel
	ragInitialized              atomic.Bool
	titleGen                    *titleGenerator
	suggestionsGen              *suggestionsGenerator
	sessionStore                SessionStore
}

type streamResult struct {
	Calls             []tools.ToolCall
	Content           string
	ReasoningContent  string
	ThinkingSignature string // Used with Anthropic's extended thinking feature
	ThoughtSignature  []byte
	Stopped           bool
}

type Opt func(*LocalRuntime)

func WithCurrentAgent(agentName string) Opt {
	return func(r *LocalRuntime) {
		r.currentAgent = agentName
	}
}

func WithManagedOAuth(managed bool) Opt {
	return func(r *LocalRuntime) {
		r.managedOAuth = managed
	}
}

// WithTracer sets a custom OpenTelemetry tracer; if not provided, tracing is disabled (no-op).
func WithTracer(t trace.Tracer) Opt {
	return func(r *LocalRuntime) {
		r.tracer = t
	}
}

func WithSessionCompaction(sessionCompaction bool) Opt {
	return func(r *LocalRuntime) {
		r.sessionCompaction = sessionCompaction
	}
}

func WithModelStore(store ModelStore) Opt {
	return func(r *LocalRuntime) {
		r.modelsStore = store
	}
}

func WithSessionStore(store SessionStore) Opt {
	return func(r *LocalRuntime) {
		r.sessionStore = store
	}
}

// New creates a new runtime for an agent and its team
func New(agents *team.Team, opts ...Opt) (*LocalRuntime, error) {
	modelsStore, err := modelsdev.NewStore()
	if err != nil {
		return nil, err
	}

	r := &LocalRuntime{
		toolMap:              make(map[string]ToolHandler),
		team:                 agents,
		currentAgent:         "root",
		resumeChan:           make(chan ResumeType),
		elicitationRequestCh: make(chan ElicitationResult),
		modelsStore:          modelsStore,
		sessionCompaction:    true,
		managedOAuth:         true,
		sessionStore:         session.NewInMemorySessionStore(),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Validate that we have at least one agent and that the current agent exists
	if _, err = r.team.Agent(r.currentAgent); err != nil {
		return nil, err
	}

	model := agents.Model()
	if model == nil {
		return nil, errors.New("no model found for the team; ensure at least one agent has a valid model")
	}

	r.titleGen = newTitleGenerator(model)

	slog.Debug("Creating new runtime", "agent", r.currentAgent, "available_agents", agents.Size())

	return r, nil
}

// StartBackgroundRAGInit initializes RAG in background and forwards events
// Should be called early (e.g., by App) to start indexing before RunStream
func (r *LocalRuntime) StartBackgroundRAGInit(ctx context.Context, sendEvent func(Event)) {
	if r.ragInitialized.Swap(true) {
		return
	}

	ragManagers := r.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Starting background RAG initialization with event forwarding", "manager_count", len(ragManagers))

	// Set up event forwarding BEFORE starting initialization
	// This ensures all events are captured
	r.forwardRAGEvents(ctx, ragManagers, sendEvent)

	// Now start initialization (events will be forwarded)
	r.team.InitializeRAG(ctx)
	r.team.StartRAGFileWatchers(ctx)
}

// forwardRAGEvents forwards RAG manager events to the given callback
// Consolidates duplicated event forwarding logic
func (r *LocalRuntime) forwardRAGEvents(ctx context.Context, ragManagers map[string]*rag.Manager, sendEvent func(Event)) {
	for _, mgr := range ragManagers {
		go func(mgr *rag.Manager) {
			ragName := mgr.Name()
			slog.Debug("Starting RAG event forwarder goroutine", "rag", ragName)
			for {
				select {
				case <-ctx.Done():
					slog.Debug("RAG event forwarder stopped", "rag", ragName)
					return
				case ragEvent, ok := <-mgr.Events():
					if !ok {
						slog.Debug("RAG events channel closed", "rag", ragName)
						return
					}

					agentName := r.currentAgent
					slog.Debug("Forwarding RAG event", "type", ragEvent.Type, "rag", ragName, "agent", agentName)

					switch ragEvent.Type {
					case ragtypes.EventTypeIndexingStarted:
						sendEvent(RAGIndexingStarted(ragName, ragEvent.StrategyName, agentName))
					case ragtypes.EventTypeIndexingProgress:
						if ragEvent.Progress != nil {
							sendEvent(RAGIndexingProgress(ragName, ragEvent.StrategyName, ragEvent.Progress.Current, ragEvent.Progress.Total, agentName))
						}
					case ragtypes.EventTypeIndexingComplete:
						sendEvent(RAGIndexingCompleted(ragName, ragEvent.StrategyName, agentName))
					case ragtypes.EventTypeUsage:
						// Convert RAG usage to TokenUsageEvent so TUI displays it
						sendEvent(TokenUsage(
							"",
							agentName,
							ragEvent.TotalTokens, // input tokens (embeddings)
							0,                    // output tokens (0 for embeddings)
							ragEvent.TotalTokens, // context length
							0,                    // context limit (not applicable)
							ragEvent.Cost,
						))
					case ragtypes.EventTypeError:
						if ragEvent.Error != nil {
							sendEvent(Error(fmt.Sprintf("RAG %s error: %v", ragName, ragEvent.Error)))
						}
					default:
						// Log unhandled events for debugging
						slog.Debug("Unhandled RAG event type", "type", ragEvent.Type, "rag", ragName)
					}
				}
			}
		}(mgr)
	}
}

// InitializeRAG is called within RunStream as a fallback when background init wasn't used
// (e.g., for exec command or API mode where there's no App)
func (r *LocalRuntime) InitializeRAG(ctx context.Context, events chan Event) {
	// If already initialized via StartBackgroundRAGInit, skip entirely
	// Event forwarding was already set up there
	if r.ragInitialized.Swap(true) {
		slog.Debug("RAG already initialized, event forwarding already active", "manager_count", len(r.team.RAGManagers()))
		return
	}

	ragManagers := r.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Setting up RAG initialization (fallback path for non-TUI)", "manager_count", len(ragManagers))

	// Set up event forwarding BEFORE starting initialization
	r.forwardRAGEvents(ctx, ragManagers, func(event Event) {
		events <- event
	})

	// Start initialization and file watchers
	r.team.InitializeRAG(ctx)
	r.team.StartRAGFileWatchers(ctx)
}

func (r *LocalRuntime) CurrentAgentName() string {
	return r.currentAgent
}

func (r *LocalRuntime) CurrentAgentCommands(context.Context) map[string]string {
	return r.CurrentAgent().Commands()
}

func (r *LocalRuntime) CurrentWelcomeMessage(context.Context) string {
	return r.CurrentAgent().WelcomeMessage()
}

// CurrentMCPPrompts returns the available MCP prompts from all active MCP toolsets
// for the current agent. It discovers prompts by calling ListPrompts on each MCP toolset
// and aggregates the results into a map keyed by prompt name.
func (r *LocalRuntime) CurrentMCPPrompts(ctx context.Context) map[string]mcptools.PromptInfo {
	prompts := make(map[string]mcptools.PromptInfo)

	// Get the current agent to access its toolsets
	currentAgent := r.CurrentAgent()
	if currentAgent == nil {
		slog.Warn("No current agent available for MCP prompt discovery")
		return prompts
	}

	// Iterate through all toolsets of the current agent
	for _, toolset := range currentAgent.ToolSets() {
		if mcpToolset := UnwrapMCPToolset(toolset); mcpToolset != nil {
			slog.Debug("Found MCP toolset", "toolset", mcpToolset)
			// Discover prompts from this MCP toolset
			mcpPrompts := r.discoverMCPPrompts(ctx, mcpToolset)

			// Merge prompts into the result map
			// If there are name conflicts, the later toolset's prompt will override
			for name, promptInfo := range mcpPrompts {
				prompts[name] = promptInfo
			}
		} else {
			slog.Debug("Toolset is not an MCP toolset", "type", fmt.Sprintf("%T", toolset))
		}
	}

	slog.Debug("Discovered MCP prompts", "agent", currentAgent.Name(), "prompt_count", len(prompts))
	return prompts
}

// discoverMCPPrompts queries an MCP toolset for available prompts and converts them
// to PromptInfo structures. This method handles the MCP protocol communication
// and gracefully handles any errors during prompt discovery.
func (r *LocalRuntime) discoverMCPPrompts(ctx context.Context, toolset *mcptools.Toolset) map[string]mcptools.PromptInfo {
	prompts := make(map[string]mcptools.PromptInfo)

	// Check if the toolset is started (required for MCP operations)
	// Note: We need to implement IsStarted() method on the MCP Toolset if it doesn't exist
	// For now, we'll proceed and handle any errors from ListPrompts

	// Call ListPrompts on the MCP toolset
	// Note: We need to implement this method on the Toolset to expose MCP prompt functionality
	mcpPrompts, err := toolset.ListPrompts(ctx)
	if err != nil {
		slog.Warn("Failed to list MCP prompts from toolset", "error", err)
		return prompts
	}

	// Convert MCP prompts to our internal format
	for _, mcpPrompt := range mcpPrompts {
		promptInfo := mcptools.PromptInfo{
			Name:        mcpPrompt.Name,
			Description: mcpPrompt.Description,
			Arguments:   make([]mcptools.PromptArgument, 0),
		}

		// Convert MCP prompt arguments if they exist
		if mcpPrompt.Arguments != nil {
			for _, arg := range mcpPrompt.Arguments {
				promptArg := mcptools.PromptArgument{
					Name:        arg.Name,
					Description: arg.Description,
					Required:    arg.Required,
				}
				promptInfo.Arguments = append(promptInfo.Arguments, promptArg)
			}
		}

		prompts[mcpPrompt.Name] = promptInfo
		slog.Debug("Discovered MCP prompt", "name", mcpPrompt.Name, "args_count", len(promptInfo.Arguments))
	}

	return prompts
}

// CurrentAgent returns the current agent
func (r *LocalRuntime) CurrentAgent() *agent.Agent {
	// We validated already that the agent exists
	current, _ := r.team.Agent(r.currentAgent)
	return current
}

// EmitStartupInfo emits initial agent, team, and toolset information for immediate sidebar display
func (r *LocalRuntime) EmitStartupInfo(ctx context.Context, events chan Event) {
	// Prevent duplicate emissions
	if r.startupInfoEmitted {
		return
	}

	a := r.CurrentAgent()

	// Emit agent information for sidebar display
	var modelID string
	if model := a.Model(); model != nil {
		modelID = model.ID()
	}
	events <- AgentInfo(a.Name(), modelID, a.Description())

	// Emit team information
	availableAgents := r.team.AgentNames()
	events <- TeamInfo(availableAgents, r.currentAgent)

	// Emit agent warnings (if any)
	r.emitAgentWarnings(a, events)

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.Warn("Failed to get agent tools during startup", "agent", a.Name(), "error", err)
		// Emit toolset info with 0 tools if we can't get them
		events <- ToolsetInfo(0, r.currentAgent)
		r.startupInfoEmitted = true
		return
	}

	// Emit toolset information
	events <- ToolsetInfo(len(agentTools), r.currentAgent)
	r.startupInfoEmitted = true
}

// registerDefaultTools registers the default tool handlers
func (r *LocalRuntime) registerDefaultTools() {
	slog.Debug("Registering default tools")

	tt := builtin.NewTransferTaskTool()
	ht := builtin.NewHandoffTool()
	ttTools, _ := tt.Tools(context.TODO())
	htTools, _ := ht.Tools(context.TODO())
	allTools := append(ttTools, htTools...)

	handlers := map[string]ToolHandlerFunc{
		builtin.ToolNameTransferTask: r.handleTaskTransfer,
		builtin.ToolNameHandoff:      r.handleHandoff,
	}

	for _, t := range allTools {
		if h, exists := handlers[t.Name]; exists {
			r.toolMap[t.Name] = ToolHandler{handler: h, tool: t}
		} else {
			slog.Warn("No handler found for default tool", "tool", t.Name)
		}
	}

	slog.Debug("Registered default tools", "count", len(r.toolMap))
}

func (r *LocalRuntime) finalizeEventChannel(ctx context.Context, sess *session.Session, events chan Event) {
	defer close(events)

	// Generate follow-up suggestions before stopping stream (if enabled)
	if r.suggestionsGen != nil {
		r.suggestionsGen.Generate(ctx, sess, events, r.currentAgent)
		r.suggestionsGen.Wait()
	}

	events <- StreamStopped(sess.ID, r.currentAgent)

	telemetry.RecordSessionEnd(ctx)

	r.titleGen.Wait()
}

// RunStream starts the agent's interaction loop and returns a channel of events
func (r *LocalRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	slog.Debug("Starting runtime stream", "agent", r.currentAgent, "session_id", sess.ID)
	events := make(chan Event, 128)

	go func() {
		telemetry.RecordSessionStart(ctx, r.currentAgent, sess.ID)

		ctx, sessionSpan := r.startSpan(ctx, "runtime.session", trace.WithAttributes(
			attribute.String("agent", r.currentAgent),
			attribute.String("session.id", sess.ID),
		))
		defer sessionSpan.End()

		// Set the events channel for elicitation requests
		r.setElicitationEventsChannel(events)
		defer r.clearElicitationEventsChannel()

		// Set elicitation handler on all MCP toolsets before getting tools
		a := r.CurrentAgent()

		// Emit agent information for sidebar display
		var modelID string
		model := a.Model()
		if model != nil {
			modelID = model.ID()
		}
		events <- AgentInfo(a.Name(), modelID, a.Description())

		// Initialize suggestions generator if enabled for this agent
		if cfg := a.FollowUpSuggestionsConfig(); cfg != nil && cfg.Enabled && model != nil {
			r.suggestionsGen = newSuggestionsGenerator(model, cfg)
		}

		// Emit team information
		availableAgents := r.team.AgentNames()
		events <- TeamInfo(availableAgents, r.currentAgent)

		// Initialize RAG and forward events
		r.InitializeRAG(ctx, events)

		r.emitAgentWarnings(a, events)

		for _, toolset := range a.ToolSets() {
			toolset.SetElicitationHandler(r.elicitationHandler)
			toolset.SetOAuthSuccessHandler(func() {
				events <- Authorization(tools.ElicitationActionAccept, r.currentAgent)
			})
			toolset.SetManagedOAuth(r.managedOAuth)
		}

		agentTools, err := r.getTools(ctx, a, sessionSpan, events)
		if err != nil {
			events <- Error(fmt.Sprintf("failed to get tools: %v", err))
			return
		}

		events <- ToolsetInfo(len(agentTools), r.currentAgent)

		messages := sess.GetMessages(a)
		if sess.SendUserMessage {
			events <- UserMessage(messages[len(messages)-1].Content)
		}

		events <- StreamStarted(sess.ID, a.Name())

		defer r.finalizeEventChannel(ctx, sess, events)

		r.registerDefaultTools()

		if sess.Title == "" {
			r.titleGen.Generate(ctx, sess, events)
		}

		iteration := 0
		// Use a runtime copy of maxIterations so we don't modify the session's persistent config
		runtimeMaxIterations := sess.MaxIterations

		for {
			// Set elicitation handler on all MCP toolsets before getting tools
			a := r.CurrentAgent()

			r.emitAgentWarnings(a, events)

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
						_ = r.sessionStore.UpdateSession(ctx, sess)
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

			var contextLimit int64
			if m != nil {
				contextLimit = int64(m.Limit.Context)
			}

			if m != nil && r.sessionCompaction {
				if sess.InputTokens+sess.OutputTokens > int64(float64(contextLimit)*0.9) {
					r.Summarize(ctx, sess, events)
					events <- TokenUsage(sess.ID, r.currentAgent, sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)
				}
			}

			messages := sess.GetMessages(a)
			slog.Debug("Retrieved messages for processing", "agent", a.Name(), "message_count", len(messages))

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
					ThoughtSignature:  res.ThoughtSignature,
					ToolCalls:         res.Calls,
					CreatedAt:         time.Now().Format(time.RFC3339),
				}

				sess.AddMessage(session.NewAgentMessage(a, &assistantMessage))
				_ = r.sessionStore.UpdateSession(ctx, sess)
				slog.Debug("Added assistant message to session", "agent", a.Name(), "total_messages", len(sess.GetAllMessages()))
			} else {
				slog.Debug("Skipping empty assistant message (no content and no tool calls)", "agent", a.Name())
			}

			events <- TokenUsage(sess.ID, r.currentAgent, sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)

			r.processToolCalls(ctx, sess, res.Calls, agentTools, events)

			if res.Stopped {
				slog.Debug("Conversation stopped", "agent", a.Name())
				break
			}
		}
	}()

	return events
}

// getTools executes tool retrieval with automatic OAuth handling
func (r *LocalRuntime) getTools(ctx context.Context, a *agent.Agent, sessionSpan trace.Span, events chan Event) ([]tools.Tool, error) {
	shouldEmitMCPInit := len(a.ToolSets()) > 0
	if shouldEmitMCPInit {
		events <- MCPInitStarted(a.Name())
	}
	defer func() {
		if shouldEmitMCPInit {
			events <- MCPInitFinished(a.Name())
		}
	}()

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.Error("Failed to get agent tools", "agent", a.Name(), "error", err)
		sessionSpan.RecordError(err)
		sessionSpan.SetStatus(codes.Error, "failed to get tools")
		telemetry.RecordError(ctx, err.Error())
		return nil, err
	}

	slog.Debug("Retrieved agent tools", "agent", a.Name(), "tool_count", len(agentTools))
	return agentTools, nil
}

func (r *LocalRuntime) emitAgentWarnings(a *agent.Agent, events chan Event) {
	warnings := a.DrainWarnings()
	if len(warnings) == 0 {
		return
	}

	slog.Warn("Tool setup partially failed; continuing", "agent", a.Name(), "warnings", warnings)

	if events != nil {
		events <- Warning(formatToolWarning(a, warnings), r.currentAgent)
	}
}

func formatToolWarning(a *agent.Agent, warnings []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Some toolsets failed to initialize for agent '%s'.\n\n", a.Name()))
	builder.WriteString("Details:\n\n")
	for _, warning := range warnings {
		builder.WriteString("- ")
		builder.WriteString(warning)
		builder.WriteByte('\n')
	}

	return strings.TrimSuffix(builder.String(), "\n")
}

func (r *LocalRuntime) Resume(_ context.Context, confirmationType ResumeType) {
	slog.Debug("Resuming runtime", "agent", r.currentAgent, "confirmation_type", confirmationType)

	cType := ResumeTypeApproveSession
	switch confirmationType {
	case ResumeTypeApprove:
		cType = ResumeTypeApprove
	case ResumeTypeReject:
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
func (r *LocalRuntime) ResumeElicitation(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
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
func (r *LocalRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}

func (r *LocalRuntime) handleStream(ctx context.Context, stream chat.MessageStream, a *agent.Agent, agentTools []tools.Tool, sess *session.Session, m *modelsdev.Model, events chan Event) (streamResult, error) {
	defer stream.Close()

	var fullContent strings.Builder
	var fullReasoningContent strings.Builder
	var thinkingSignature string
	var thoughtSignature []byte
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
				cost := float64(response.Usage.InputTokens)*m.Cost.Input +
					float64(response.Usage.OutputTokens)*m.Cost.Output +
					float64(response.Usage.CachedInputTokens)*m.Cost.CacheRead +
					float64(response.Usage.CacheWriteTokens)*m.Cost.CacheWrite
				sess.Cost += cost / 1e6
			}

			sess.InputTokens = response.Usage.InputTokens + response.Usage.CachedInputTokens + response.Usage.CacheWriteTokens
			sess.OutputTokens = response.Usage.OutputTokens

			modelName := "unknown"
			if m != nil {
				modelName = m.Name
			}
			telemetry.RecordTokenUsage(ctx, modelName, sess.InputTokens, sess.OutputTokens, sess.Cost)
		}

		if len(response.Choices) == 0 {
			continue
		}
		choice := response.Choices[0]

		if len(choice.Delta.ThoughtSignature) > 0 {
			thoughtSignature = choice.Delta.ThoughtSignature
		}

		if choice.FinishReason == chat.FinishReasonStop || choice.FinishReason == chat.FinishReasonLength {
			return streamResult{
				Calls:             toolCalls,
				Content:           fullContent.String(),
				ReasoningContent:  fullReasoningContent.String(),
				ThinkingSignature: thinkingSignature,
				ThoughtSignature:  thoughtSignature,
				Stopped:           true,
			}, nil
		}

		// Handle tool calls
		if len(choice.Delta.ToolCalls) > 0 {
			// Process each tool call delta
			for _, deltaToolCall := range choice.Delta.ToolCalls {
				// Find existing tool call by ID, or create a new one
				idx := -1
				for i, toolCall := range toolCalls {
					if toolCall.ID == deltaToolCall.ID {
						idx = i
						break
					}
				}

				// If tool call doesn't exist yet, append it
				if idx == -1 {
					idx = len(toolCalls)
					toolCalls = append(toolCalls, tools.ToolCall{
						ID:   deltaToolCall.ID,
						Type: deltaToolCall.Type,
					})
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
		ThoughtSignature:  thoughtSignature,
		Stopped:           stoppedDueToNoOutput,
	}, nil
}

// processToolCalls handles the execution of tool calls for an agent
func (r *LocalRuntime) processToolCalls(ctx context.Context, sess *session.Session, calls []tools.ToolCall, agentTools []tools.Tool, events chan Event) {
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
		def, exists := r.toolMap[toolCall.Function.Name]
		if exists {
			slog.Debug("Using runtime tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
			// TODO: make this better, these tools define themselves as read-only
			if sess.ToolsApproved || def.tool.Annotations.ReadOnlyHint {
				r.runAgentTool(callCtx, def.handler, sess, toolCall, def.tool, events, a)
			} else {
				slog.Debug("Tools not approved, waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)

				events <- ToolCallConfirmation(toolCall, def.tool, a.Name())

				select {
				case cType := <-r.resumeChan:
					switch cType {
					case ResumeTypeApprove:
						slog.Debug("Resume signal received, approving tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.runAgentTool(callCtx, def.handler, sess, toolCall, def.tool, events, a)
					case ResumeTypeApproveSession:
						slog.Debug("Resume signal received, approving session", "tool", toolCall.Function.Name, "session_id", sess.ID)
						sess.ToolsApproved = true
						r.runAgentTool(callCtx, def.handler, sess, toolCall, def.tool, events, a)
					case ResumeTypeReject:
						slog.Debug("Resume signal received, rejecting tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)
						r.addToolRejectedResponse(ctx, sess, toolCall, def.tool, events)
					}
				case <-callCtx.Done():
					slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
					// Synthesize cancellation responses for the current and any remaining tool calls
					r.addToolCancelledResponse(ctx, sess, toolCall, def.tool, events)
					for j := i + 1; j < len(calls); j++ {
						r.addToolCancelledResponse(ctx, sess, calls[j], def.tool, events)
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
						r.addToolRejectedResponse(ctx, sess, toolCall, tool, events)
					}

					slog.Debug("Added tool response to session", "tool", toolCall.Function.Name, "session_id", sess.ID, "total_messages", len(sess.GetAllMessages()))
					break toolLoop
				case <-callCtx.Done():
					slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
					// Synthesize cancellation responses for the current and any remaining tool calls
					r.addToolCancelledResponse(ctx, sess, toolCall, tool, events)
					for j := i + 1; j < len(calls); j++ {
						r.addToolCancelledResponse(ctx, sess, calls[j], tool, events)
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
func (r *LocalRuntime) runTool(ctx context.Context, tool tools.Tool, toolCall tools.ToolCall, events chan Event, sess *session.Session, a *agent.Agent) {
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
	_ = r.sessionStore.UpdateSession(ctx, sess)
}

func (r *LocalRuntime) runAgentTool(ctx context.Context, handler ToolHandlerFunc, sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event, a *agent.Agent) {
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
	_ = r.sessionStore.UpdateSession(ctx, sess)
}

func (r *LocalRuntime) addToolRejectedResponse(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event) {
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
	_ = r.sessionStore.UpdateSession(ctx, sess)
}

func (r *LocalRuntime) addToolCancelledResponse(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, tool tools.Tool, events chan Event) {
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
	_ = r.sessionStore.UpdateSession(ctx, sess)
}

// startSpan wraps tracer.Start, returning a no-op span if the tracer is nil.
func (r *LocalRuntime) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if r.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return r.tracer.Start(ctx, name, opts...)
}

func (r *LocalRuntime) handleTaskTransfer(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, evts chan Event) (*tools.ToolCallResult, error) {
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

	// Emit agent switching start event
	evts <- AgentSwitching(true, ca, params.Agent)

	r.currentAgent = params.Agent
	defer func() {
		r.currentAgent = ca

		// Emit agent switching end event
		evts <- AgentSwitching(false, params.Agent, ca)

		// Restore original agent info in sidebar
		if originalAgent, err := r.team.Agent(ca); err == nil {
			var modelID string
			if model := originalAgent.Model(); model != nil {
				modelID = model.ID()
			}
			evts <- AgentInfo(originalAgent.Name(), modelID, originalAgent.Description())
		}
	}()

	// Emit agent info for the new agent
	if newAgent, err := r.team.Agent(params.Agent); err == nil {
		var modelID string
		if model := newAgent.Model(); model != nil {
			modelID = model.ID()
		}
		evts <- AgentInfo(newAgent.Name(), modelID, newAgent.Description())
	}

	memberAgentTask := "You are a member of a team of agents. Your goal is to complete the following task:"
	memberAgentTask += fmt.Sprintf("\n\n<task>\n%s\n</task>", params.Task)
	if params.ExpectedOutput != "" {
		memberAgentTask += fmt.Sprintf("\n\n<expected_output>\n%s\n</expected_output>", params.ExpectedOutput)
	}

	slog.Debug("Creating new session with parent session", "parent_session_id", sess.ID, "tools_approved", sess.ToolsApproved)

	child, err := r.team.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	s := session.New(
		session.WithSystemMessage(memberAgentTask),
		session.WithImplicitUserMessage("Follow the default instructions"),
		session.WithMaxIterations(child.MaxIterations()),
		session.WithTitle("Transferred task"),
		session.WithToolsApproved(sess.ToolsApproved),
		session.WithSendUserMessage(false),
	)

	for event := range r.RunStream(ctx, s) {
		evts <- event
		if errEvent, ok := event.(*ErrorEvent); ok {
			span.RecordError(fmt.Errorf("%s", errEvent.Error))
			span.SetStatus(codes.Error, "error in transferred session")
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	sess.ToolsApproved = s.ToolsApproved

	sess.AddSubSession(s)

	slog.Debug("Task transfer completed", "agent", params.Agent, "task", params.Task)

	span.SetStatus(codes.Ok, "task transfer completed")
	return &tools.ToolCallResult{
		Output: s.GetLastAssistantMessageContent(),
	}, nil
}

func (r *LocalRuntime) handleHandoff(_ context.Context, _ *session.Session, toolCall tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
	var params builtin.HandoffArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ca := r.currentAgent
	next, err := r.team.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	r.currentAgent = next.Name()
	return &tools.ToolCallResult{
		Output: fmt.Sprintf("The agent %s handed off the conversation to you, look at the history of the conversation and continue where it left off. Once you are done with your task or if the user asks you, handoff the conversation back to %s.", ca, ca),
	}, nil
}

// Summarize generates a summary for the session based on the conversation history
func (r *LocalRuntime) Summarize(ctx context.Context, sess *session.Session, events chan Event) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", r.currentAgent)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", r.currentAgent)
	}()

	// Create conversation history for summarization
	var conversationHistory strings.Builder
	messages := sess.GetAllMessages()

	// Check if session is empty
	if len(messages) == 0 {
		events <- &WarningEvent{Message: "Session is empty. Start a conversation before compacting."}
		return
	}
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
	newModel := provider.CloneWithOptions(ctx, r.CurrentAgent().Model(), options.WithStructuredOutput(nil))
	newTeam := team.New(
		team.WithAgents(agent.New("root", systemPrompt, agent.WithModel(newModel))),
	)

	summarySession := session.New(session.WithSystemMessage(systemPrompt))
	summarySession.AddMessage(session.UserMessage(userPrompt))
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
	_ = r.sessionStore.UpdateSession(ctx, sess)
	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, r.currentAgent)
}

// setElicitationEventsChannel sets the current events channel for elicitation requests
func (r *LocalRuntime) setElicitationEventsChannel(events chan Event) {
	r.elicitationEventsChannelMux.Lock()
	defer r.elicitationEventsChannelMux.Unlock()
	r.elicitationEventsChannel = events
}

// clearElicitationEventsChannel clears the current events channel
func (r *LocalRuntime) clearElicitationEventsChannel() {
	r.elicitationEventsChannelMux.Lock()
	defer r.elicitationEventsChannelMux.Unlock()
	r.elicitationEventsChannel = nil
}

// elicitationHandler creates an elicitation handler that can be used by MCP clients
// This handler propagates elicitation requests to the runtime's client via events
func (r *LocalRuntime) elicitationHandler(ctx context.Context, req *mcp.ElicitParams) (tools.ElicitationResult, error) {
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
