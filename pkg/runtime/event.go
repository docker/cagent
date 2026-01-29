package runtime

import (
	"cmp"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/types"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

type Event interface {
	GetAgentName() string
}

// AgentContext carries optional agent attribution for an event.
type AgentContext struct {
	AgentName string `json:"agent_name,omitempty"`
}

// GetAgentName returns the agent name for events embedding AgentContext.
func (a AgentContext) GetAgentName() string { return a.AgentName }

// UserMessageEvent is sent when a user message is received
type UserMessageEvent struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

func (e *UserMessageEvent) GetAgentName() string { return "" }

func UserMessage(message, sessionID string) Event {
	return &UserMessageEvent{
		Type:      "user_message",
		Message:   message,
		SessionID: sessionID,
	}
}

// PartialToolCallEvent is sent when a tool call is first received (partial/complete)
type PartialToolCallEvent struct {
	Type           string         `json:"type"`
	ToolCall       tools.ToolCall `json:"tool_call"`
	ToolDefinition tools.Tool     `json:"tool_definition"`
	AgentContext
}

func PartialToolCall(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string) Event {
	return &PartialToolCallEvent{
		Type:           "partial_tool_call",
		ToolCall:       toolCall,
		ToolDefinition: toolDefinition,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

// ToolCallEvent is sent when a tool call is received
type ToolCallEvent struct {
	Type           string         `json:"type"`
	ToolCall       tools.ToolCall `json:"tool_call"`
	ToolDefinition tools.Tool     `json:"tool_definition"`
	AgentContext
}

func ToolCall(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string) Event {
	return &ToolCallEvent{
		Type:           "tool_call",
		ToolCall:       toolCall,
		ToolDefinition: toolDefinition,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

type ToolCallConfirmationEvent struct {
	Type           string         `json:"type"`
	ToolCall       tools.ToolCall `json:"tool_call"`
	ToolDefinition tools.Tool     `json:"tool_definition"`
	AgentContext
}

func ToolCallConfirmation(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string) Event {
	return &ToolCallConfirmationEvent{
		Type:           "tool_call_confirmation",
		ToolCall:       toolCall,
		ToolDefinition: toolDefinition,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

type ToolCallResponseEvent struct {
	Type           string                `json:"type"`
	ToolCall       tools.ToolCall        `json:"tool_call"`
	ToolDefinition tools.Tool            `json:"tool_definition"`
	Response       string                `json:"response"`
	Result         *tools.ToolCallResult `json:"result,omitempty"`
	AgentContext
}

func ToolCallResponse(toolCall tools.ToolCall, toolDefinition tools.Tool, result *tools.ToolCallResult, response, agentName string) Event {
	return &ToolCallResponseEvent{
		Type:           "tool_call_response",
		ToolCall:       toolCall,
		Response:       response,
		Result:         result,
		ToolDefinition: toolDefinition,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

type StreamStartedEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	AgentContext
}

func StreamStarted(sessionID, agentName string) Event {
	return &StreamStartedEvent{
		Type:         "stream_started",
		SessionID:    sessionID,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type AgentChoiceEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	AgentContext
}

func AgentChoice(agentName, content string) Event {
	return &AgentChoiceEvent{
		Type:         "agent_choice",
		Content:      content,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type AgentChoiceReasoningEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	AgentContext
}

func AgentChoiceReasoning(agentName, content string) Event {
	return &AgentChoiceReasoningEvent{
		Type:         "agent_choice_reasoning",
		Content:      content,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type ErrorEvent struct {
	Type  string `json:"type"`
	Error string `json:"error"`
	AgentContext
}

func Error(msg string) Event {
	return &ErrorEvent{
		Type:  "error",
		Error: msg,
	}
}

type ShellOutputEvent struct {
	Type   string `json:"type"`
	Output string `json:"error"`
}

func (e *ShellOutputEvent) GetAgentName() string { return "" }

func ShellOutput(output string) Event {
	return &ShellOutputEvent{
		Type:   "shell",
		Output: output,
	}
}

type WarningEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	AgentContext
}

func Warning(message, agentName string) Event {
	return &WarningEvent{
		Type:         "warning",
		Message:      message,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type TokenUsageEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Usage     *Usage `json:"usage"`
	AgentContext
}

type Usage struct {
	InputTokens   int64         `json:"input_tokens"`
	OutputTokens  int64         `json:"output_tokens"`
	ContextLength int64         `json:"context_length"`
	ContextLimit  int64         `json:"context_limit"`
	Cost          float64       `json:"cost"`
	LastMessage   *MessageUsage `json:"last_message,omitempty"`
}

// MessageUsage contains per-message usage data to include in TokenUsageEvent.
// It embeds chat.Usage and adds Cost and Model fields.
type MessageUsage struct {
	chat.Usage
	chat.RateLimit
	Cost  float64
	Model string
}

func TokenUsage(sessionID, agentName string, inputTokens, outputTokens, contextLength, contextLimit int64, cost float64) Event {
	return TokenUsageWithMessage(sessionID, agentName, inputTokens, outputTokens, contextLength, contextLimit, cost, nil)
}

func TokenUsageWithMessage(sessionID, agentName string, inputTokens, outputTokens, contextLength, contextLimit int64, cost float64, msgUsage *MessageUsage) Event {
	return &TokenUsageEvent{
		Type:      "token_usage",
		SessionID: sessionID,
		Usage: &Usage{
			ContextLength: contextLength,
			ContextLimit:  contextLimit,
			InputTokens:   inputTokens,
			OutputTokens:  outputTokens,
			Cost:          cost,
			LastMessage:   msgUsage,
		},
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type SessionTitleEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	AgentContext
}

func SessionTitle(sessionID, title string) Event {
	return &SessionTitleEvent{
		Type:      "session_title",
		SessionID: sessionID,
		Title:     title,
	}
}

type SessionSummaryEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	AgentContext
}

func SessionSummary(sessionID, summary, agentName string) Event {
	return &SessionSummaryEvent{
		Type:         "session_summary",
		SessionID:    sessionID,
		Summary:      summary,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type SessionCompactionEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	AgentContext
}

func SessionCompaction(sessionID, status, agentName string) Event {
	return &SessionCompactionEvent{
		Type:         "session_compaction",
		SessionID:    sessionID,
		Status:       status,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type StreamStoppedEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	AgentContext
}

func StreamStopped(sessionID, agentName string) Event {
	return &StreamStoppedEvent{
		Type:         "stream_stopped",
		SessionID:    sessionID,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

// ElicitationRequestEvent is sent when an elicitation request is received from an MCP server
type ElicitationRequestEvent struct {
	Type          string         `json:"type"`
	Message       string         `json:"message"`
	Mode          string         `json:"mode,omitempty"` // "form" or "url"
	Schema        any            `json:"schema,omitempty"`
	URL           string         `json:"url,omitempty"`
	ElicitationID string         `json:"elicitation_id,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
	AgentContext
}

func ElicitationRequest(message, mode string, schema any, url, elicitationID string, meta map[string]any, agentName string) Event {
	return &ElicitationRequestEvent{
		Type:          "elicitation_request",
		Message:       message,
		Mode:          mode,
		Schema:        schema,
		URL:           url,
		ElicitationID: elicitationID,
		Meta:          meta,
		AgentContext:  AgentContext{AgentName: agentName},
	}
}

type AuthorizationEvent struct {
	Type         string                  `json:"type"`
	Confirmation tools.ElicitationAction `json:"confirmation"`
	AgentContext
}

func Authorization(confirmation tools.ElicitationAction, agentName string) Event {
	return &AuthorizationEvent{
		Type:         "authorization_event",
		Confirmation: confirmation,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type MaxIterationsReachedEvent struct {
	Type          string `json:"type"`
	MaxIterations int    `json:"max_iterations"`
	AgentContext
}

func MaxIterationsReached(maxIterations int) Event {
	return &MaxIterationsReachedEvent{
		Type:          "max_iterations_reached",
		MaxIterations: maxIterations,
	}
}

// MCPInitStartedEvent is for MCP initialization lifecycle events
type MCPInitStartedEvent struct {
	Type string `json:"type"`
	AgentContext
}

func MCPInitStarted(agentName string) Event {
	return &MCPInitStartedEvent{
		Type:         "mcp_init_started",
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type MCPInitFinishedEvent struct {
	Type string `json:"type"`
	AgentContext
}

func MCPInitFinished(agentName string) Event {
	return &MCPInitFinishedEvent{
		Type:         "mcp_init_finished",
		AgentContext: AgentContext{AgentName: agentName},
	}
}

// AgentInfoEvent is sent when agent information is available or changes
type AgentInfoEvent struct {
	Type           string `json:"type"`
	AgentName      string `json:"agent_name"`
	Model          string `json:"model"` // this is in provider/model format (e.g., "openai/gpt-4o")
	Description    string `json:"description"`
	WelcomeMessage string `json:"welcome_message,omitempty"`
	AgentContext
}

func AgentInfo(agentName, model, description, welcomeMessage string) Event {
	return &AgentInfoEvent{
		Type:           "agent_info",
		AgentName:      agentName,
		Model:          model,
		Description:    description,
		WelcomeMessage: welcomeMessage,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

// AgentDetails contains information about an agent for display in the sidebar
type AgentDetails struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	Commands    types.Commands `json:"commands,omitempty"`
}

// TeamInfoEvent is sent when team information is available
type TeamInfoEvent struct {
	Type            string         `json:"type"`
	AvailableAgents []AgentDetails `json:"available_agents"`
	CurrentAgent    string         `json:"current_agent"`
	AgentContext
}

func TeamInfo(availableAgents []AgentDetails, currentAgent string) Event {
	return &TeamInfoEvent{
		Type:            "team_info",
		AvailableAgents: availableAgents,
		CurrentAgent:    currentAgent,
		AgentContext:    AgentContext{AgentName: currentAgent},
	}
}

// AgentSwitchingEvent is sent when agent switching starts or stops
type AgentSwitchingEvent struct {
	Type      string `json:"type"`
	Switching bool   `json:"switching"`
	FromAgent string `json:"from_agent,omitempty"`
	ToAgent   string `json:"to_agent,omitempty"`
	AgentContext
}

func AgentSwitching(switching bool, fromAgent, toAgent string) Event {
	return &AgentSwitchingEvent{
		Type:         "agent_switching",
		Switching:    switching,
		FromAgent:    fromAgent,
		ToAgent:      toAgent,
		AgentContext: AgentContext{AgentName: cmp.Or(toAgent, fromAgent)},
	}
}

// ToolsetInfoEvent is sent when toolset information is available
// When Loading is true, more tools may still be loading (e.g., MCP servers starting)
type ToolsetInfoEvent struct {
	Type           string `json:"type"`
	AvailableTools int    `json:"available_tools"`
	Loading        bool   `json:"loading"`
	AgentContext
}

func ToolsetInfo(availableTools int, loading bool, agentName string) Event {
	return &ToolsetInfoEvent{
		Type:           "toolset_info",
		AvailableTools: availableTools,
		Loading:        loading,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

// RAGIndexingStartedEvent is for RAG lifecycle events
type RAGIndexingStartedEvent struct {
	Type         string `json:"type"`
	RAGName      string `json:"rag_name"`
	StrategyName string `json:"strategy_name"`
	AgentContext
}

func RAGIndexingStarted(ragName, strategyName, agentName string) Event {
	return &RAGIndexingStartedEvent{
		Type:         "rag_indexing_started",
		RAGName:      ragName,
		StrategyName: strategyName,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type RAGIndexingProgressEvent struct {
	Type         string `json:"type"`
	RAGName      string `json:"rag_name"`
	StrategyName string `json:"strategy_name"`
	Current      int    `json:"current"`
	Total        int    `json:"total"`
	AgentContext
}

func RAGIndexingProgress(ragName, strategyName string, current, total int, agentName string) Event {
	return &RAGIndexingProgressEvent{
		Type:         "rag_indexing_progress",
		RAGName:      ragName,
		StrategyName: strategyName,
		Current:      current,
		Total:        total,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

type RAGIndexingCompletedEvent struct {
	Type         string `json:"type"`
	RAGName      string `json:"rag_name"`
	StrategyName string `json:"strategy_name"`
	AgentContext
}

func RAGIndexingCompleted(ragName, strategyName, agentName string) Event {
	return &RAGIndexingCompletedEvent{
		Type:         "rag_indexing_completed",
		RAGName:      ragName,
		StrategyName: strategyName,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

// HookBlockedEvent is sent when a pre-tool hook blocks a tool call
type HookBlockedEvent struct {
	Type           string         `json:"type"`
	ToolCall       tools.ToolCall `json:"tool_call"`
	ToolDefinition tools.Tool     `json:"tool_definition"`
	Message        string         `json:"message"`
	AgentContext
}

func HookBlocked(toolCall tools.ToolCall, toolDefinition tools.Tool, message, agentName string) Event {
	return &HookBlockedEvent{
		Type:           "hook_blocked",
		ToolCall:       toolCall,
		ToolDefinition: toolDefinition,
		Message:        message,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

// MessageAddedEvent is emitted when a message is added to the session.
// This event is used by the PersistentRuntime wrapper to persist messages.
type MessageAddedEvent struct {
	Type      string           `json:"type"`
	SessionID string           `json:"session_id"`
	Message   *session.Message `json:"-"`
	AgentContext
}

func (e *MessageAddedEvent) GetAgentName() string { return e.AgentName }

func MessageAdded(sessionID string, msg *session.Message, agentName string) Event {
	return &MessageAddedEvent{
		Type:         "message_added",
		SessionID:    sessionID,
		Message:      msg,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

// SubSessionCompletedEvent is emitted when a sub-session completes and is added to parent.
// This event is used by the PersistentRuntime wrapper to persist sub-sessions.
type SubSessionCompletedEvent struct {
	Type            string `json:"type"`
	ParentSessionID string `json:"parent_session_id"`
	SubSession      any    `json:"sub_session"` // *session.Session
	AgentContext
}

func (e *SubSessionCompletedEvent) GetAgentName() string { return e.AgentName }

func SubSessionCompleted(parentSessionID string, subSession any, agentName string) Event {
	return &SubSessionCompletedEvent{
		Type:            "sub_session_completed",
		ParentSessionID: parentSessionID,
		SubSession:      subSession,
		AgentContext:    AgentContext{AgentName: agentName},
	}
}
