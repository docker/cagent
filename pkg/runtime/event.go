package runtime

import (
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
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *UserMessageEvent) GetAgentName() string { return "" }

func UserMessage(message string) Event {
	return &UserMessageEvent{
		Type:    "user_message",
		Message: message,
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
	Type           string         `json:"type"`
	ToolCall       tools.ToolCall `json:"tool_call"`
	ToolDefinition tools.Tool     `json:"tool_definition"`
	Response       string         `json:"response"`
	AgentContext
}

func ToolCallResponse(toolCall tools.ToolCall, toolDefinition tools.Tool, response, agentName string) Event {
	return &ToolCallResponseEvent{
		Type:           "tool_call_response",
		ToolCall:       toolCall,
		Response:       response,
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
	// Type stays "token_usage" for backward compatibility with existing clients.
	Type string `json:"type"`
	// SessionID lets consumers correlate usage snapshots with a specific session/sub-session.
	SessionID string `json:"session_id"`
	// Usage retains the legacy aggregate payload so older UIs do not break immediately.
	Usage *Usage `json:"usage,omitempty"`
	// SelfUsage captures the tokens/cost generated directly by the emitting session.
	SelfUsage *Usage `json:"self_usage,omitempty"`
	// InclusiveUsage represents the session plus any merged child usage for team totals.
	InclusiveUsage *Usage `json:"inclusive_usage,omitempty"`
	AgentContext
}

type Usage struct {
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	ContextLength int     `json:"context_length"`
	ContextLimit  int     `json:"context_limit"`
	Cost          float64 `json:"cost"`
}

func TokenUsage(sessionID, agentName string, selfUsage, inclusiveUsage *Usage) Event {
	if selfUsage == nil && inclusiveUsage == nil {
		return &TokenUsageEvent{Type: "token_usage"}
	}

	// Default to inclusive usage when only one snapshot is provided.
	if selfUsage == nil {
		selfUsage = inclusiveUsage
	}
	if inclusiveUsage == nil {
		inclusiveUsage = selfUsage
	}

	// Emit both snapshots so the UI can show per-session and team totals simultaneously.
	return &TokenUsageEvent{
		Type:           "token_usage",
		SessionID:      sessionID,
		Usage:          inclusiveUsage,
		SelfUsage:      selfUsage,
		InclusiveUsage: inclusiveUsage,
		AgentContext:   AgentContext{AgentName: agentName},
	}
}

type SessionTitleEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	AgentContext
}

func SessionTitle(sessionID, title, agentName string) Event {
	return &SessionTitleEvent{
		Type:         "session_title",
		SessionID:    sessionID,
		Title:        title,
		AgentContext: AgentContext{AgentName: agentName},
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
	Type    string         `json:"type"`
	Message string         `json:"message"`
	Schema  any            `json:"schema"`
	Meta    map[string]any `json:"meta,omitempty"`
	AgentContext
}

func ElicitationRequest(message string, schema any, meta map[string]any, agentName string) Event {
	return &ElicitationRequestEvent{
		Type:         "elicitation_request",
		Message:      message,
		Schema:       schema,
		Meta:         meta,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

func (e *ElicitationRequestEvent) GetAgentName() string { return e.AgentName }

type AuthorizationEvent struct {
	Type         string `json:"type"`
	Confirmation string `json:"confirmation"` // only "confirmed"
	AgentContext
}

func (e *AuthorizationEvent) GetAgentName() string { return "" }

func Authorization(confirmation, agentName string) Event {
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

func (e *MaxIterationsReachedEvent) GetAgentName() string {
	return e.AgentName
}

// MCP initialization lifecycle events
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
