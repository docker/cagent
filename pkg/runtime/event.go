package runtime

import (
	"github.com/docker/cagent/pkg/tools"
)

type Event interface {
	isEvent()
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

func UserMessage(message string) Event {
	return &UserMessageEvent{
		Type:    "user_message",
		Message: message,
	}
}

func (e *UserMessageEvent) GetAgentName() string {
	return ""
}

func (e *UserMessageEvent) isEvent() {}

// ToolCallEvent is sent when a tool call is received
// PartialToolCallEvent is sent when a tool call is first received (partial/complete)
type PartialToolCallEvent struct {
	Type     string         `json:"type"`
	ToolCall tools.ToolCall `json:"tool_call"`
	AgentContext
}

func PartialToolCall(toolCall tools.ToolCall, agentName string) Event {
	return &PartialToolCallEvent{
		Type:         "partial_tool_call",
		ToolCall:     toolCall,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

func (e *PartialToolCallEvent) isEvent() {}

type ToolCallEvent struct {
	Type     string         `json:"type"`
	ToolCall tools.ToolCall `json:"tool_call"`
	AgentContext
}

func ToolCall(toolCall tools.ToolCall, agentName string) Event {
	return &ToolCallEvent{
		Type:         "tool_call",
		ToolCall:     toolCall,
		AgentContext: AgentContext{AgentName: agentName},
	}
}

func (e *ToolCallEvent) isEvent() {}

type ToolCallConfirmationEvent struct {
	Type     string         `json:"type"`
	ToolCall tools.ToolCall `json:"tool_call"`
	AgentContext
}

func ToolCallConfirmation(toolCall tools.ToolCall, agentName string) Event {
	return &ToolCallConfirmationEvent{
		Type:         "tool_call_confirmation",
		ToolCall:     toolCall,
		AgentContext: AgentContext{AgentName: agentName},
	}
}
func (e *ToolCallConfirmationEvent) isEvent() {}

type ToolCallResponseEvent struct {
	Type     string         `json:"type"`
	ToolCall tools.ToolCall `json:"tool_call"`
	Response string         `json:"response"`
	AgentContext
}

func ToolCallResponse(toolCall tools.ToolCall, response, agentName string) Event {
	return &ToolCallResponseEvent{
		Type:         "tool_call_response",
		ToolCall:     toolCall,
		Response:     response,
		AgentContext: AgentContext{AgentName: agentName},
	}
}
func (e *ToolCallResponseEvent) isEvent() {}

type StreamStartedEvent struct {
	Type string `json:"type"`
}

func StreamStarted() Event {
	return &StreamStartedEvent{
		Type: "stream_started",
	}
}

func (e *StreamStartedEvent) GetAgentName() string {
	return ""
}

func (e *StreamStartedEvent) isEvent() {}

type AgentChoiceEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	AgentContext
}

func AgentChoice(agentName string, content string) Event { //nolint:gocritic
	return &AgentChoiceEvent{
		Type:         "agent_choice",
		Content:      content,
		AgentContext: AgentContext{AgentName: agentName},
	}
}
func (e *AgentChoiceEvent) isEvent() {}

type AgentChoiceReasoningEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	AgentContext
}

func AgentChoiceReasoning(agentName string, content string) Event { //nolint:gocritic
	return &AgentChoiceReasoningEvent{
		Type:         "agent_choice_reasoning",
		Content:      content,
		AgentContext: AgentContext{AgentName: agentName},
	}
}
func (e *AgentChoiceReasoningEvent) isEvent() {}

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
func (e *ErrorEvent) isEvent() {}

type ShellOutputEvent struct {
	Type   string `json:"type"`
	Output string `json:"error"`
}

func ShellOutput(output string) Event {
	return &ShellOutputEvent{
		Type:   "shell",
		Output: output,
	}
}
func (e *ShellOutputEvent) isEvent()             {}
func (e *ShellOutputEvent) GetAgentName() string { return "" }

type TokenUsageEvent struct {
	Type  string `json:"type"`
	Usage *Usage `json:"usage"`
	AgentContext
}

type Usage struct {
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	ContextLength int     `json:"context_length"`
	ContextLimit  int     `json:"context_limit"`
	Cost          float64 `json:"cost"`
}

func TokenUsage(inputTokens, outputTokens, contextLength, contextLimit int, cost float64) Event {
	return &TokenUsageEvent{
		Type: "token_usage",
		Usage: &Usage{
			ContextLength: contextLength,
			ContextLimit:  contextLimit,
			InputTokens:   inputTokens,
			OutputTokens:  outputTokens,
			Cost:          cost,
		},
	}
}
func (e *TokenUsageEvent) isEvent() {}

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
func (e *SessionTitleEvent) isEvent() {}

type SessionSummaryEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	AgentContext
}

func SessionSummary(sessionID, summary string) Event {
	return &SessionSummaryEvent{
		Type:      "session_summary",
		SessionID: sessionID,
		Summary:   summary,
	}
}
func (e *SessionSummaryEvent) isEvent() {}

type SessionCompactionEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	AgentContext
}

func SessionCompaction(sessionID, status string) Event {
	return &SessionCompactionEvent{
		Type:      "session_compaction",
		SessionID: sessionID,
		Status:    status,
	}
}
func (e *SessionCompactionEvent) isEvent() {}

type StreamStoppedEvent struct {
	Type string `json:"type"`
}

func StreamStopped() Event {
	return &StreamStoppedEvent{
		Type: "stream_stopped",
	}
}

func (e *StreamStoppedEvent) GetAgentName() string {
	return ""
}

func (e *StreamStoppedEvent) isEvent() {}

type AuthorizationRequiredEvent struct {
	Type         string `json:"type"`
	ServerURL    string `json:"server_url"`
	ServerType   string `json:"server_type"`
	Confirmation string `json:"confirmation"` // only  "pending" | "confirmed" | "denied"
}

func AuthorizationRequired(serverURL, serverType, confirmation string) Event {
	return &AuthorizationRequiredEvent{
		Type:         "authorization_required",
		ServerURL:    serverURL,
		ServerType:   serverType,
		Confirmation: confirmation,
	}
}

func (e *AuthorizationRequiredEvent) isEvent() {}

func (e *AuthorizationRequiredEvent) GetAgentName() string {
	return ""
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

func (e *MaxIterationsReachedEvent) isEvent() {}

func (e *MaxIterationsReachedEvent) GetAgentName() string {
	return e.AgentName
}
