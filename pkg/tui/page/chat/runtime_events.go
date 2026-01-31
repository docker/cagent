package chat

import (
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	msgtypes "github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/types"
)

// Runtime Event Handling
//
// This file maps runtime events to UI updates, following the Elm Architecture
// pattern of explicit event-to-update mappings. Events are organized by category:
//
// Stream Lifecycle:
//   - StreamStartedEvent  → Start spinners, set pending response
//   - StreamStoppedEvent  → Stop spinners, process queue, maybe exit
//
// Content Events:
//   - AgentChoiceEvent         → Append text to message
//   - AgentChoiceReasoningEvent → Append reasoning block
//   - UserMessageEvent         → Replace loading with user message
//
// Tool Events:
//   - PartialToolCallEvent      → Show tool call in progress
//   - ToolCallEvent             → Tool execution started
//   - ToolCallConfirmationEvent → Show confirmation dialog
//   - ToolCallResponseEvent     → Show tool result
//
// Sidebar Updates (forwarded):
//   - TokenUsageEvent, AgentInfoEvent, TeamInfoEvent, etc.
//
// Dialogs:
//   - MaxIterationsReachedEvent → Show max iterations dialog
//   - ElicitationRequestEvent   → Show elicitation/OAuth dialog

// handleRuntimeEvent processes runtime events and returns the appropriate command.
// Returns (handled, cmd) where handled indicates if the event was processed.
//
// The switch is organized by event category for clarity.
func (p *chatPage) handleRuntimeEvent(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	// ===== Error and Warning Events =====
	case *runtime.ErrorEvent:
		return true, p.messages.AddErrorMessage(msg.Error)

	case *runtime.WarningEvent:
		return true, notification.WarningCmd(msg.Message)

	// ===== Stream Lifecycle Events =====
	case *runtime.StreamStartedEvent:
		return true, p.handleStreamStarted(msg)

	case *runtime.StreamStoppedEvent:
		return true, p.handleStreamStopped(msg)

	// ===== Content Events =====
	case *runtime.UserMessageEvent:
		return true, p.messages.ReplaceLoadingWithUser(msg.Message)

	case *runtime.AgentChoiceEvent:
		return true, p.handleAgentChoice(msg)

	case *runtime.AgentChoiceReasoningEvent:
		return true, p.handleAgentChoiceReasoning(msg)

	case *runtime.ShellOutputEvent:
		return true, p.messages.AddShellOutputMessage(msg.Output)

	// ===== Tool Events =====
	case *runtime.PartialToolCallEvent:
		return true, p.handlePartialToolCall(msg)

	case *runtime.ToolCallEvent:
		return true, p.handleToolCall(msg)

	case *runtime.ToolCallConfirmationEvent:
		return true, p.handleToolCallConfirmation(msg)

	case *runtime.ToolCallResponseEvent:
		return true, p.handleToolCallResponse(msg)

	// ===== Sidebar Info Events (forwarded) =====
	case *runtime.TokenUsageEvent:
		p.handleTokenUsage(msg)
		return true, nil

	case *runtime.AgentInfoEvent:
		p.sidebar.SetAgentInfo(msg.AgentName, msg.Model, msg.Description)
		p.messages.AddWelcomeMessage(msg.WelcomeMessage)
		return true, nil

	case *runtime.TeamInfoEvent:
		p.sidebar.SetTeamInfo(msg.AvailableAgents)
		return true, nil

	case *runtime.AgentSwitchingEvent:
		p.sidebar.SetAgentSwitching(msg.Switching)
		return true, nil

	case *runtime.ToolsetInfoEvent:
		p.sidebar.SetToolsetInfo(msg.AvailableTools, msg.Loading)
		return true, nil

	case *runtime.SessionTitleEvent:
		return true, p.forwardToSidebar(msg)

	case *runtime.SessionCompactionEvent:
		if msg.Status == "completed" {
			return true, notification.SuccessCmd("Session compacted successfully.")
		}
		return true, nil

	// ===== RAG Indexing Events (forwarded to sidebar) =====
	case *runtime.RAGIndexingStartedEvent,
		*runtime.RAGIndexingProgressEvent,
		*runtime.RAGIndexingCompletedEvent:
		return true, p.forwardToSidebar(msg)

	// ===== Dialog Events =====
	case *runtime.MaxIterationsReachedEvent:
		return true, p.handleMaxIterationsReached(msg)

	case *runtime.ElicitationRequestEvent:
		return true, p.handleElicitationRequest(msg)
	}

	return false, nil
}

// forwardToSidebar forwards a message to the sidebar and returns the resulting command.
func (p *chatPage) forwardToSidebar(msg tea.Msg) tea.Cmd {
	slog.Debug("Forwarding event to sidebar", "event_type", fmt.Sprintf("%T", msg))
	model, cmd := p.sidebar.Update(msg)
	p.sidebar = model.(sidebar.Model)
	return cmd
}

// handleTokenUsage updates sidebar and session with token usage data.
// This handler performs side effects only and returns no command.
func (p *chatPage) handleTokenUsage(msg *runtime.TokenUsageEvent) {
	p.sidebar.SetTokenUsage(msg)
	if msg.Usage != nil {
		if sess := p.app.Session(); sess != nil {
			// Update session-level totals
			sess.InputTokens = msg.Usage.InputTokens
			sess.OutputTokens = msg.Usage.OutputTokens
			sess.Cost = msg.Usage.Cost

			// Track per-message usage for /cost dialog
			if msg.Usage.LastMessage != nil {
				sess.AddMessageUsageRecord(
					msg.AgentName,
					msg.Usage.LastMessage.Model,
					msg.Usage.LastMessage.Cost,
					&msg.Usage.LastMessage.Usage,
				)
			}
		}
	}
}

func (p *chatPage) handleStreamStarted(msg *runtime.StreamStartedEvent) tea.Cmd {
	slog.Debug("handleStreamStarted called", "agent", msg.AgentName, "session_id", msg.SessionID)
	p.streamCancelled = false
	spinnerCmd := p.setWorking(true)
	pendingCmd := p.setPendingResponse(true)
	p.startProgressBar()
	sidebarCmd := p.forwardToSidebar(msg)
	return tea.Batch(pendingCmd, spinnerCmd, sidebarCmd)
}

func (p *chatPage) handleAgentChoice(msg *runtime.AgentChoiceEvent) tea.Cmd {
	if p.streamCancelled {
		return nil
	}
	// Track that we've received assistant content
	p.hasReceivedAssistantContent = true
	// Clear pending response indicator - first chunk has arrived
	p.setPendingResponse(false)
	return p.messages.AppendToLastMessage(msg.AgentName, msg.Content)
}

func (p *chatPage) handleAgentChoiceReasoning(msg *runtime.AgentChoiceReasoningEvent) tea.Cmd {
	if p.streamCancelled {
		return nil
	}
	p.setPendingResponse(false)
	return p.messages.AppendReasoning(msg.AgentName, msg.Content)
}

func (p *chatPage) handleStreamStopped(msg *runtime.StreamStoppedEvent) tea.Cmd {
	slog.Debug("handleStreamStopped called",
		"agent", msg.AgentName,
		"session_id", msg.SessionID,
		"should_exit", p.app.ShouldExitAfterFirstResponse(),
		"has_content", p.hasReceivedAssistantContent)
	spinnerCmd := p.setWorking(false)
	p.setPendingResponse(false)
	if p.msgCancel != nil {
		p.msgCancel = nil
	}
	p.streamCancelled = false
	p.stopProgressBar()
	sidebarCmd := p.forwardToSidebar(msg)

	// Check if there are queued messages to process
	queueCmd := p.processNextQueuedMessage()

	// Check if we should exit after this response
	// Only exit if we've actually received assistant content (not just on any stream stop)
	var exitCmd tea.Cmd
	if p.app.ShouldExitAfterFirstResponse() && p.hasReceivedAssistantContent {
		slog.Debug("Exit after first response triggered, scheduling delayed exit")
		exitCmd = tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
			return msgtypes.ExitAfterFirstResponseMsg{}
		})
	}

	return tea.Batch(p.messages.ScrollToBottom(), spinnerCmd, sidebarCmd, queueCmd, exitCmd)
}

// handlePartialToolCall processes partial tool call events by rendering each
// tool call as it streams in. The tool call appears with its name and a static
// "pending" indicator (not animated) to show it's receiving data.
func (p *chatPage) handlePartialToolCall(msg *runtime.PartialToolCallEvent) tea.Cmd {
	p.setPendingResponse(false)
	toolCmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusPending)
	return tea.Batch(toolCmd, p.messages.ScrollToBottom())
}

func (p *chatPage) handleToolCallConfirmation(msg *runtime.ToolCallConfirmationEvent) tea.Cmd {
	spinnerCmd := p.setWorking(false)
	toolCmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusConfirmation)
	dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewToolConfirmationDialog(msg, p.sessionState),
	})
	return tea.Batch(toolCmd, p.messages.ScrollToBottom(), spinnerCmd, dialogCmd)
}

func (p *chatPage) handleToolCall(msg *runtime.ToolCallEvent) tea.Cmd {
	p.setPendingResponse(false)
	spinnerCmd := p.setWorking(true)
	toolCmd := p.messages.AddOrUpdateToolCall(msg.AgentName, msg.ToolCall, msg.ToolDefinition, types.ToolStatusRunning)
	return tea.Batch(toolCmd, p.messages.ScrollToBottom(), spinnerCmd)
}

func (p *chatPage) handleToolCallResponse(msg *runtime.ToolCallResponseEvent) tea.Cmd {
	spinnerCmd := p.setWorking(true)

	status := types.ToolStatusCompleted
	if msg.Result.IsError {
		status = types.ToolStatusError
	}
	toolCmd := p.messages.AddToolResult(msg, status)

	// Update todo sidebar if this is a todo tool
	if msg.ToolDefinition.Category == "todo" && !msg.Result.IsError {
		_ = p.sidebar.SetTodos(msg.Result)
	}

	return tea.Batch(toolCmd, p.messages.ScrollToBottom(), spinnerCmd)
}

func (p *chatPage) handleMaxIterationsReached(msg *runtime.MaxIterationsReachedEvent) tea.Cmd {
	spinnerCmd := p.setWorking(false)
	dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewMaxIterationsDialog(msg.MaxIterations, p.app),
	})
	return tea.Batch(spinnerCmd, dialogCmd)
}

func (p *chatPage) handleElicitationRequest(msg *runtime.ElicitationRequestEvent) tea.Cmd {
	spinnerCmd := p.setWorking(false)

	// Check if this is an OAuth flow by looking at the meta type
	// Guard against nil Meta map to prevent panic
	if msg.Meta != nil {
		if elicitationType, ok := msg.Meta["cagent/type"].(string); ok && elicitationType == "oauth_flow" {
			// OAuth flow - show the OAuth authorization dialog
			var serverURL string
			if url, ok := msg.Meta["cagent/server_url"].(string); ok {
				serverURL = url
			}
			dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
				Model: dialog.NewOAuthAuthorizationDialog(serverURL, p.app),
			})
			return tea.Batch(spinnerCmd, dialogCmd)
		}
	}

	// Check elicitation mode
	switch msg.Mode {
	case "url":
		// URL-based elicitation - show URL dialog
		dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewURLElicitationDialog(msg.Message, msg.URL),
		})
		return tea.Batch(spinnerCmd, dialogCmd)

	default:
		// Form-based elicitation (default) - show form dialog
		dialogCmd := core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewElicitationDialog(msg.Message, msg.Schema, msg.Meta),
		})
		return tea.Batch(spinnerCmd, dialogCmd)
	}
}
